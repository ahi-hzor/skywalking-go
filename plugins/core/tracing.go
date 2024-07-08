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

package core

import (
	"fmt"
	"github.com/pkg/errors"
	"reflect"
	"runtime/debug"

	"github.com/apache/skywalking-go/plugins/core/reporter"
	"github.com/apache/skywalking-go/plugins/core/tracing"
)

var snapshotType = reflect.TypeOf(&SnapshotSpan{})

func (t *Tracer) Tracing() interface{} {
	return t
}

func (t *Tracer) Logger() interface{} {
	return t.Log
}

func (t *Tracer) DebugStack() []byte {
	return debug.Stack()
}

func (t *Tracer) CreateEntrySpan(operationName string, extractor interface{}, opts ...interface{}) (s interface{}, err error) {
	fmt.Println("create entry span start =>", operationName)
	ctx, tracingSpan, noop := t.createNoop(operationName)
	if noop {
		fmt.Println("create entry span noop")
		return tracingSpan, nil
	}
	defer func() {
		fmt.Println("save entry span to active")
		saveSpanToActiveIfNotError(ctx, s, err)
	}()
	// if parent span is entry span, then use parent span as result
	if tracingSpan != nil && tracingSpan.IsEntry() && reflect.ValueOf(tracingSpan).Type() != snapshotType {
		fmt.Println("parent span is entry span")
		tracingSpan.SetOperationName(operationName)
		return tracingSpan, nil
	}
	var ref = &SpanContext{}
	if err := ref.Decode(extractor.(tracing.ExtractorWrapper).Fun()); err != nil {
		fmt.Printf("decode entry span ref error %v", err)
		return nil, err
	}
	if !ref.Valid {
		fmt.Println("entry span ref invalid")
		ref = nil
	}

	return t.createSpan0(ctx, tracingSpan, opts, withRef(ref), withSpanType(SpanTypeEntry), withOperationName(operationName))
}

func (t *Tracer) CreateLocalSpan(operationName string, opts ...interface{}) (s interface{}, err error) {
	ctx, tracingSpan, noop := t.createNoop(operationName)
	if noop {
		return tracingSpan, nil
	}
	defer func() {
		saveSpanToActiveIfNotError(ctx, s, err)
	}()

	return t.createSpan0(ctx, tracingSpan, opts, withSpanType(SpanTypeLocal), withOperationName(operationName))
}

func (t *Tracer) CreateExitSpan(operationName, peer string, injector interface{}, opts ...interface{}) (s interface{}, err error) {
	fmt.Println("create exit span start =>", operationName)
	ctx, tracingSpan, noop := t.createNoop(operationName)
	if noop {
		return tracingSpan, nil
	}
	defer func() {
		fmt.Println("save exit span to active")
		saveSpanToActiveIfNotError(ctx, s, err)
	}()

	// if parent span is exit span, then use parent span as result
	if tracingSpan != nil && tracingSpan.IsExit() && reflect.ValueOf(tracingSpan).Type() != snapshotType {
		fmt.Println("parent span is exit span")
		return tracingSpan, nil
	}
	span, err := t.createSpan0(ctx, tracingSpan, opts, withSpanType(SpanTypeExit), withOperationName(operationName), withPeer(peer))
	if err != nil {
		return nil, err
	}
	spanContext := &SpanContext{}
	reportedSpan, ok := span.(SegmentSpan)
	if !ok {
		fmt.Println("exit span type is wrong")
		return nil, errors.New("span type is wrong")
	}

	firstSpan := reportedSpan.GetSegmentContext().FirstSpan
	spanContext.Sample = 1
	spanContext.TraceID = reportedSpan.GetSegmentContext().TraceID
	spanContext.ParentSegmentID = reportedSpan.GetSegmentContext().SegmentID
	spanContext.ParentSpanID = reportedSpan.GetSegmentContext().SpanID
	spanContext.ParentService = t.ServiceEntity.ServiceName
	spanContext.ParentServiceInstance = t.ServiceEntity.ServiceInstanceName
	spanContext.ParentEndpoint = firstSpan.GetOperationName()
	spanContext.AddressUsedAtClient = peer
	spanContext.CorrelationContext = reportedSpan.GetSegmentContext().CorrelationContext

	err = spanContext.Encode(injector.(tracing.InjectorWrapper).Fun())
	if err != nil {
		fmt.Printf("Encode exit span error %v", err)
		return nil, err
	}
	return span, nil
}

func (t *Tracer) ActiveSpan() interface{} {
	ctx := getTracingContext()
	if ctx == nil || ctx.ActiveSpan() == nil {
		return nil
	}
	span := ctx.ActiveSpan()
	return span
}

func (t *Tracer) GetRuntimeContextValue(key string) interface{} {
	context := getTracingContext()
	if context == nil {
		return nil
	}
	return context.Runtime.Get(key)
}

func (t *Tracer) SetRuntimeContextValue(key string, value interface{}) {
	context := getTracingContext()
	if context == nil {
		context = NewTracingContext()
		SetGLS(context)
	}
	context.Runtime.Set(key, value)
}

func (t *Tracer) CaptureContext() interface{} {
	ctx := getTracingContext()
	if ctx == nil {
		return nil
	}
	snapshot := &ContextSnapshot{
		activeSpan: newSnapshotSpan(ctx.ActiveSpan()),
		runtime:    ctx.Runtime.clone(),
	}
	return snapshot
}

func (t *Tracer) ContinueContext(snapshot interface{}) {
	if snapshot == nil {
		return
	}
	if snap, ok := snapshot.(*ContextSnapshot); ok {
		ctx := getTracingContext()
		if ctx == nil {
			ctx = NewTracingContext()
			SetGLS(ctx)
		}
		ctx.activeSpan = snap.activeSpan
		ctx.Runtime = snap.runtime
	}
}

func (t *Tracer) CleanContext() {
	SetGLS(nil)
}

func (t *Tracer) GetCorrelationContextValue(key string) string {
	span := t.ActiveSpan()
	if span == nil {
		return ""
	}
	switch reportedSpan := span.(type) {
	case *SegmentSpanImpl:
		return reportedSpan.Context().GetCorrelationContextValue(key)
	case *RootSegmentSpan:
		return reportedSpan.Context().GetCorrelationContextValue(key)
	default:
		return ""
	}
}

func (t *Tracer) SetCorrelationContextValue(key, value string) {
	span := t.ActiveSpan()
	if span == nil {
		return
	}
	switch reportedSpan := span.(type) {
	case *SegmentSpanImpl:
		if len(value) > t.correlation.MaxValueSize {
			return
		}
		if len(reportedSpan.GetSegmentContext().CorrelationContext) >= t.correlation.MaxKeyCount {
			return
		}
		reportedSpan.Context().SetCorrelationContextValue(key, value)
	case *RootSegmentSpan:
		if len(value) > t.correlation.MaxValueSize {
			return
		}
		if len(reportedSpan.GetSegmentContext().CorrelationContext) >= t.correlation.MaxKeyCount {
			return
		}
		reportedSpan.Context().SetCorrelationContextValue(key, value)
	default:
	}
}

type ContextSnapshot struct {
	activeSpan TracingSpan
	runtime    *RuntimeContext
}

func (s *ContextSnapshot) IsValid() bool {
	return s.activeSpan != nil && s.runtime != nil
}

func (t *Tracer) createNoop(operationName string) (*TracingContext, TracingSpan, bool) {
	if !t.InitSuccess() || t.Reporter.ConnectionStatus() == reporter.ConnectionStatusDisconnect {
		fmt.Println("ConnectionStatusDisconnect")
		return nil, newNoopSpan(), true
	}
	fmt.Println("tracing ignore start=>", operationName, t.ignoreSuffix, t.traceIgnorePath)
	if tracerIgnore(operationName, t.ignoreSuffix, t.traceIgnorePath) {
		fmt.Println("tracerIgnore=>true")
		return nil, newNoopSpan(), true
	}
	fmt.Println("tracerIgnore=>false")
	ctx := getTracingContext()
	if ctx != nil {
		span := ctx.ActiveSpan()
		noop, ok := span.(*NoopSpan)
		if ok {
			fmt.Println("TracingContext ActiveSpan")
			// increase the stack count for ensure the noop span can be clear in the context
			noop.enterNoSpan()
		}
		return ctx, span, ok
	}
	fmt.Println("NewTracingContext")
	ctx = NewTracingContext()
	return ctx, nil, false
}

func (t *Tracer) createSpan0(ctx *TracingContext, parent TracingSpan, pluginOpts []interface{}, coreOpts ...interface{}) (s TracingSpan, err error) {
	ds := NewDefaultSpan(t, parent)
	var parentSpan SegmentSpan
	if parent != nil {
		tmpSpan, ok := parent.(SegmentSpan)
		if ok {
			fmt.Println("parentSpan ok")
			parentSpan = tmpSpan
		}
	}
	isForceSample := len(ds.Refs) > 0
	// Try to sample when it is not force sample
	if parentSpan == nil && !isForceSample {
		// Force sample
		sampled := t.Sampler.IsSampled(ds.OperationName)
		if !sampled {
			// Filter by sample just return noop span
			fmt.Println("span  sample ignore")
			return newNoopSpan(), nil
		}
	}
	// process the opts from agent core for prepare building segment span
	for _, opt := range coreOpts {
		opt.(tracing.SpanOption).Apply(ds)
	}
	s, err = NewSegmentSpan(ctx, ds, parentSpan)
	if err != nil {
		fmt.Printf("NewSegmentSpan error %v", err)
		return nil, err
	}
	// process the opts from plugin, split opts because the DefaultSpan not contains the tracing context information(AdaptSpan)
	for _, opt := range pluginOpts {
		opt.(tracing.SpanOption).Apply(s)
	}
	return s, nil
}

func withSpanType(spanType SpanType) tracing.SpanOption {
	return buildSpanOption(func(span *DefaultSpan) {
		span.SpanType = spanType
	})
}

func withOperationName(opName string) tracing.SpanOption {
	return buildSpanOption(func(span *DefaultSpan) {
		span.OperationName = opName
	})
}

func withRef(sc reporter.SpanContext) tracing.SpanOption {
	return buildSpanOption(func(span *DefaultSpan) {
		if sc == nil {
			return
		}
		v := reflect.ValueOf(sc)
		if v.Interface() == reflect.Zero(v.Type()).Interface() {
			return
		}
		span.Refs = append(span.Refs, sc)
	})
}

func withPeer(peer string) tracing.SpanOption {
	return buildSpanOption(func(span *DefaultSpan) {
		span.Peer = peer
	})
}

type spanOpImpl struct {
	exe func(s *DefaultSpan)
}

func (s *spanOpImpl) Apply(span interface{}) {
	if segmentSpan, ok := span.(*DefaultSpan); ok {
		s.exe(segmentSpan)
	}
}

func buildSpanOption(e func(s *DefaultSpan)) tracing.SpanOption {
	return &spanOpImpl{exe: e}
}

func getTracingContext() *TracingContext {
	gls := GetGLS()
	if gls == nil {
		return nil
	}
	return gls.(*TracingContext)
}

func saveSpanToActiveIfNotError(ctx *TracingContext, span interface{}, err error) {
	if err != nil || span == nil {
		return
	}
	ctx.SaveActiveSpan(span.(TracingSpan))
	SetGLS(ctx)
}
