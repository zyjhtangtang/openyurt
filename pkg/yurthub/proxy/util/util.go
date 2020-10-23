/*
Copyright 2020 The OpenYurt Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/alibaba/openyurt/pkg/yurthub/kubernetes/serializer"
	"github.com/alibaba/openyurt/pkg/yurthub/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog"
)

const (
	canCacheHeader string = "Edge-Cache"
)

// WithRequestContentType add req-content-type in request context.
// if no Accept header is set, application/vnd.kubernetes.protobuf will be used
func WithRequestContentType(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		if info, ok := apirequest.RequestInfoFrom(ctx); ok {
			if info.IsResourceRequest {
				var contentType string
				header := req.Header.Get("Accept")
				parts := strings.Split(header, ",")
				if len(parts) >= 1 {
					contentType = parts[0]
				}

				if len(contentType) == 0 {
					klog.Errorf("no accept content type for request: %s", util.ReqString(req))
					http.Error(w, "no accept content type is set.", http.StatusBadRequest)
					return
				}

				ctx = util.WithReqContentType(ctx, contentType)
				req = req.WithContext(ctx)
			}
		}

		handler.ServeHTTP(w, req)
	})
}

// WithCacheHeaderCheck add cache agent for response cache
// in default mode, only kubelet, kube-proxy, flanneld, coredns User-Agent
// can be supported to cache response. and with Edge-Cache header is also supported.
func WithCacheHeaderCheck(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		if info, ok := apirequest.RequestInfoFrom(ctx); ok {
			if info.IsResourceRequest {
				needToCache := strings.ToLower(req.Header.Get(canCacheHeader))
				if needToCache == "true" {
					ctx = util.WithReqCanCache(ctx, true)
					req = req.WithContext(ctx)
				}
				req.Header.Del(canCacheHeader)
			}
		}

		handler.ServeHTTP(w, req)
	})
}

// WithRequestClientComponent add component field in request context.
// component is extracted from User-Agent Header, and only the content
// before the "/" when User-Agent include "/".
func WithRequestClientComponent(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		if info, ok := apirequest.RequestInfoFrom(ctx); ok {
			if info.IsResourceRequest {
				var comp string
				userAgent := strings.ToLower(req.Header.Get("User-Agent"))
				parts := strings.Split(userAgent, "/")
				if len(parts) > 0 {
					comp = strings.ToLower(parts[0])
				}

				if comp != "" {
					ctx = util.WithClientComponent(ctx, comp)
					req = req.WithContext(ctx)
				}
			}
		}

		handler.ServeHTTP(w, req)
	})
}

type wrapperResponseWriter struct {
	http.ResponseWriter
	statusCode    int
	closeNotifyCh chan bool
	ctx           context.Context
}

func newWrapperResponseWriter(ctx context.Context, rw http.ResponseWriter) *wrapperResponseWriter {
	return &wrapperResponseWriter{
		ResponseWriter: rw,
		closeNotifyCh:  make(chan bool, 1),
		ctx:            ctx,
	}
}

func (wrw *wrapperResponseWriter) Write(b []byte) (int, error) {
	return wrw.ResponseWriter.Write(b)
}

func (wrw *wrapperResponseWriter) Header() http.Header {
	return wrw.ResponseWriter.Header()
}

func (wrw *wrapperResponseWriter) WriteHeader(statusCode int) {
	wrw.statusCode = statusCode
	wrw.ResponseWriter.WriteHeader(statusCode)
}

func (wrw *wrapperResponseWriter) CloseNotify() <-chan bool {
	if cn, ok := wrw.ResponseWriter.(http.CloseNotifier); ok {
		return cn.CloseNotify()
	}
	klog.Infof("can't get http.CloseNotifier from http.ResponseWriter")
	go func() {
		<-wrw.ctx.Done()
		wrw.closeNotifyCh <- true
	}()

	return wrw.closeNotifyCh
}

func (wrw *wrapperResponseWriter) Flush() {
	if flusher, ok := wrw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	} else {
		klog.Errorf("can't get http.Flusher from http.ResponseWriter")
	}
}

// WithRequestTrace used for tracing in flight requests. and when in flight
// requests exceeds the threshold, the following incoming requests will be rejected.
func WithRequestTrace(handler http.Handler, limit int) http.Handler {
	var reqChan chan bool
	if limit > 0 {
		reqChan = make(chan bool, limit)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		wrapperRW := newWrapperResponseWriter(req.Context(), w)
		start := time.Now()

		select {
		case reqChan <- true:
			defer func() {
				<-reqChan
				klog.Infof("%s with status code %d, spent %v, left %d requests in flight", util.ReqString(req), wrapperRW.statusCode, time.Since(start), len(reqChan))
			}()
			handler.ServeHTTP(wrapperRW, req)
		default:
			// Return a 429 status indicating "Too Many Requests"
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too many requests, please try again later.", http.StatusTooManyRequests)
		}
	})
}

func WithRequestNodeLabel(handler http.Handler, labels map[string]string) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		info, _ := apirequest.RequestInfoFrom(ctx)
		//if create node, add label.
		if info.Resource == "nodes" && info.Verb == "create" {
			klog.Infof("create node, add node label: %v", labels)
			reqContentType, _ := util.ReqContentTypeFrom(ctx)
			// "application/vnd.kubernetes.protobuf"
			s, err := serializer.YurtHubSerializer.CreateSerializers(reqContentType, info.APIGroup, info.APIVersion)
			if err != nil {
				responsewriters.InternalError(w, req, fmt.Errorf("failed to create serializers: %v", err))
				return
			}

			var buf bytes.Buffer
			_, err = buf.ReadFrom(req.Body)
			if err != nil {
				responsewriters.InternalError(w, req, fmt.Errorf("Read from request body fail: %v", err))
				return
			}
			obj, _, err := s.Decoder.Decode(buf.Bytes(), nil, nil)
			if err != nil {
				responsewriters.InternalError(w, req, fmt.Errorf("Fail decode request : %v", err))
				return
			}

			node, ok := obj.(*v1.Node)
			if !ok {
				responsewriters.InternalError(w, req, fmt.Errorf("Create node fail: Assertion v1.node type fail."))
				return
			}

			if node.Labels == nil {
				node.Labels = labels
			} else {
				for key, value := range labels {
					node.Labels[key] = value
				}
			}

			newBuf := bytes.NewBuffer([]byte{})
			if err := s.Encoder.Encode(node, newBuf); err != nil {
				responsewriters.InternalError(w, req, fmt.Errorf("Encoder node fail: %v", err))
				return
			}
			req.Body = ioutil.NopCloser(newBuf)
			req.ContentLength = int64(newBuf.Len())
		}
		handler.ServeHTTP(w, req)
	})
}
