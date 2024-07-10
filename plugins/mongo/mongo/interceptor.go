// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package mongo

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo/options"
	"strings"

	"github.com/apache/skywalking-go/plugins/core/log"
	"github.com/apache/skywalking-go/plugins/core/operator"
	"github.com/apache/skywalking-go/plugins/core/tracing"
)

type NewClientInterceptor struct {
}

var removeFieldsInStmt = map[string]*struct{}{
	"lsid":         nil,
	"$clusterTime": nil,
	"txnNumber":    nil,
}

func (m *NewClientInterceptor) BeforeInvoke(invocation operator.Invocation) error {
	opts := invocation.Args()[0].([]*options.ClientOptions)
	//syncMap := tools.NewSyncMap()
	for _, opt := range opts {
		hosts := opt.Hosts
		hostLength := len(hosts)
		// must contains host
		if hostLength == 0 {
			continue
		}
		configuredMonitor := opt.Monitor

		// overwrite monitor, if define multiple opts, it should only keep the latest on the mongo client
		opt.Monitor = &event.CommandMonitor{
			Started: func(ctx context.Context, startedEvent *event.CommandStartedEvent) {
				if configuredMonitor != nil {
					configuredMonitor.Started(ctx, startedEvent)
				}
				host := hosts[0]
				if hostLength > 1 {
					if infoSplit := strings.Index(startedEvent.ConnectionID, "["); infoSplit > 0 && strings.HasSuffix(startedEvent.ConnectionID, "]") {
						host = startedEvent.ConnectionID[0:infoSplit]
					}
				}
				span, err := tracing.CreateExitSpan("MongoDB/"+startedEvent.CommandName, host, func(headerKey, headerValue string) error {
					return nil
				}, tracing.WithComponent(42),
					tracing.WithLayer(tracing.SpanLayerDatabase),
					tracing.WithTag(tracing.TagDBType, "MongoDB"))
				if err != nil {
					log.Warnf("cannot create exit span on mongo client: %v", err)
					return
				}

				if config.CollectStatement {
					span.Tag(tracing.TagDBStatement, m.gettingStatements(startedEvent))
				}
				if config.Debug {
					fmt.Printf("put mongo request [%v] to runtime contex", startedEvent.RequestID)
				}
				tracing.SetRuntimeContextValue(fmt.Sprintf("mongo-req-%d", startedEvent.RequestID), span)
				//syncMap.Put(fmt.Sprintf("%d", startedEvent.RequestID), span)

				/*fmt.Println("force start event tracing ending.")
				span.End()*/
			},
			Succeeded: func(ctx context.Context, succeededEvent *event.CommandSucceededEvent) {
				if configuredMonitor != nil {
					configuredMonitor.Succeeded(ctx, succeededEvent)
				}
				if config.Debug {
					fmt.Printf("invoke mongo succeededEvent request [%v].", succeededEvent.RequestID)
				}
				requestKey := fmt.Sprintf("mongo-req-%d", succeededEvent.RequestID)
				span := tracing.GetRuntimeContextValue(requestKey)
				//if span, ok := syncMap.Remove(fmt.Sprintf("%d", succeededEvent.RequestID)); ok && span != nil {
				if span != nil {
					if config.Debug {
						fmt.Printf("mongo succeededEvent request [%v] span end", succeededEvent.RequestID)
					}
					span.(tracing.Span).End()
					tracing.SetRuntimeContextValue(requestKey, nil)
				} else {
					if config.Debug {
						fmt.Printf("mongo succeededEvent request [%v] span empty", succeededEvent.RequestID)
					}
				}
			},
			Failed: func(ctx context.Context, failedEvent *event.CommandFailedEvent) {
				if configuredMonitor != nil {
					configuredMonitor.Failed(ctx, failedEvent)
				}
				if config.Debug {
					fmt.Printf("invoke mongo failedEvent request [%v].", failedEvent.RequestID)
				}
				//if span, ok := syncMap.Remove(fmt.Sprintf("%d", failedEvent.RequestID)); ok && span != nil {
				requestKey := fmt.Sprintf("mongo-req-%d", failedEvent.RequestID)
				span := tracing.GetRuntimeContextValue(requestKey)
				if span != nil {
					if config.Debug {
						fmt.Printf("mongo failedEvent request [%v] span end", failedEvent.RequestID)
					}
					span.(tracing.Span).Error(failedEvent.Failure)
					span.(tracing.Span).End()
					tracing.SetRuntimeContextValue(requestKey, nil)
				} else {
					if config.Debug {
						fmt.Printf("mongo failedEvent request [%v] span empty", failedEvent.RequestID)
					}
				}
			},
		}
	}
	return nil
}

func (m *NewClientInterceptor) AfterInvoke(invocation operator.Invocation, result ...interface{}) error {
	return nil
}

func (m *NewClientInterceptor) gettingStatements(startedEvent *event.CommandStartedEvent) string {
	rows := make(bson.RawElement, 0)
	elements, err := startedEvent.Command.Elements()
	if err != nil {
		return ""
	}
	for _, element := range elements {
		if _, ok := removeFieldsInStmt[element.Key()]; !ok {
			rows = append(rows, element...)
		}
	}
	return rows.String()
}
