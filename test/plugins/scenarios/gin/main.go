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

package main

import (
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	_ "github.com/apache/skywalking-go"
)

func main() {
	engine := gin.New()
	engine.Handle("GET", "/consumer", func(context *gin.Context) {
		request, err := http.NewRequest("GET", "http://localhost:8080/provider", nil)
		if err != nil {
			log.Print(err)
			context.Status(http.StatusInternalServerError)
			return
		}

		request.Header.Set("h1", "h1-value")
		request.Header.Set("h2", "h2-value")

		client := &http.Client{}
		resp, err := client.Do(request)
		if err != nil {
			log.Print(err)
			context.Status(http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Print(err)
			context.Status(http.StatusInternalServerError)
			return
		}
		context.String(200, string(body))
	})

	engine.Handle("GET", "/provider", func(context *gin.Context) {
		context.String(200, "success")
	})

	engine.Handle("GET", "health", func(context *gin.Context) {
		context.Status(http.StatusOK)
	})

	_ = engine.Run(":8080")
}
