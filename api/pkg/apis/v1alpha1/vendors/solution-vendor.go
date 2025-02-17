/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 * SPDX-License-Identifier: MIT
 */

package vendors

import (
	"context"
	"encoding/json"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/solution"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/managers"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/observability"
	observ_utils "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/observability/utils"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/pubsub"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/vendors"
	"github.com/valyala/fasthttp"
)

type SolutionVendor struct {
	vendors.Vendor
	SolutionManager *solution.SolutionManager
}

func (o *SolutionVendor) GetInfo() vendors.VendorInfo {
	return vendors.VendorInfo{
		Version:  o.Vendor.Version,
		Name:     "Solution",
		Producer: "Microsoft",
	}
}

func (e *SolutionVendor) Init(config vendors.VendorConfig, factories []managers.IManagerFactroy, providers map[string]map[string]providers.IProvider, pubsubProvider pubsub.IPubSubProvider) error {
	err := e.Vendor.Init(config, factories, providers, pubsubProvider)
	if err != nil {
		return err
	}
	for _, m := range e.Managers {
		if c, ok := m.(*solution.SolutionManager); ok {
			e.SolutionManager = c
		}
	}
	if e.SolutionManager == nil {
		return v1alpha2.NewCOAError(nil, "solution manager is not supplied", v1alpha2.MissingConfig)
	}
	return nil
}

func (o *SolutionVendor) GetEndpoints() []v1alpha2.Endpoint {
	route := "solution"
	if o.Route != "" {
		route = o.Route
	}
	return []v1alpha2.Endpoint{
		{
			Methods: []string{fasthttp.MethodPost, fasthttp.MethodGet, fasthttp.MethodDelete},
			Route:   route + "/instances", //this route is to support ITargetProvider interface via a proxy provider
			Version: o.Version,
			Handler: o.onApplyDeployment,
		},
		{
			Methods:    []string{fasthttp.MethodPost},
			Route:      route + "/reconcile",
			Version:    o.Version,
			Parameters: []string{"delete?"},
			Handler:    o.onReconcile,
		},
		{
			Methods: []string{fasthttp.MethodGet, fasthttp.MethodPost},
			Route:   route + "/queue",
			Version: o.Version,
			Handler: o.onQueue,
		},
	}
}
func (c *SolutionVendor) onQueue(request v1alpha2.COARequest) v1alpha2.COAResponse {
	rContext, span := observability.StartSpan("Solution Vendor", request.Context, &map[string]string{
		"method": "onQueue",
	})
	defer span.End()

	sLog.Infof("V (Solution): onQueue, method: %s, traceId: %s", request.Method, span.SpanContext().TraceID().String())

	scope, exist := request.Parameters["scope"]
	if !exist {
		scope = "default"
	}
	switch request.Method {
	case fasthttp.MethodGet:
		ctx, span := observability.StartSpan("onQueue-GET", rContext, nil)
		defer span.End()
		instance := request.Parameters["instance"]

		if instance == "" {
			sLog.Infof("V (Solution): onQueue failed - 400 instance parameter is not found, traceId: %s", span.SpanContext().TraceID().String())
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State:       v1alpha2.BadRequest,
				Body:        []byte("{\"result\":\"400 - instance parameter is not found\"}"),
				ContentType: "application/json",
			})
		}
		summary, err := c.SolutionManager.GetSummary(ctx, instance, scope)
		data, _ := json.Marshal(summary)
		if err != nil {
			sLog.Infof("V (Solution): onQueue failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
			if v1alpha2.IsNotFound(err) {
				return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
					State: v1alpha2.NotFound,
					Body:  data,
				})
			} else {
				return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
					State: v1alpha2.InternalError,
					Body:  data,
				})
			}
		}
		return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State:       v1alpha2.OK,
			Body:        data,
			ContentType: "application/json",
		})
	case fasthttp.MethodPost:
		_, span := observability.StartSpan("onQueue-POST", rContext, nil)
		defer span.End()
		instance := request.Parameters["instance"]
		delete := request.Parameters["delete"]
		target := request.Parameters["target"]
		if instance == "" {
			sLog.Infof("V (Solution): onQueue failed - 400 instance parameter is not found, traceId: %s", span.SpanContext().TraceID().String())
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State:       v1alpha2.BadRequest,
				Body:        []byte("{\"result\":\"400 - instance parameter is not found\"}"),
				ContentType: "application/json",
			})
		}
		action := "UPDATE"
		if delete == "true" {
			action = "DELETE"
		}
		objType := "instance"
		if target == "true" {
			objType = "target"
		}
		c.Vendor.Context.Publish("job", v1alpha2.Event{
			Metadata: map[string]string{
				"objectType": objType,
				"scope":      scope,
			},
			Body: v1alpha2.JobData{
				Id:     instance,
				Action: action,
			},
		})
		return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State:       v1alpha2.OK,
			Body:        []byte("{\"result\":\"200 - instance reconcilation job accepted\"}"),
			ContentType: "application/json",
		})
	}
	sLog.Infof("V (Solution): onQueue failed - 405 method not allowed, traceId: %s", span.SpanContext().TraceID().String())
	return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	})
}
func (c *SolutionVendor) onReconcile(request v1alpha2.COARequest) v1alpha2.COAResponse {
	rContext, span := observability.StartSpan("Solution Vendor", request.Context, &map[string]string{
		"method": "onReconcile",
	})
	defer span.End()

	sLog.Infof("V (Solution): onReconcile, method: %s, traceId: %s", request.Method, span.SpanContext().TraceID().String())
	scope, exist := request.Parameters["scope"]
	if !exist {
		scope = "default"
	}
	switch request.Method {
	case fasthttp.MethodPost:
		ctx, span := observability.StartSpan("onReconcile-POST", rContext, nil)
		defer span.End()
		var deployment model.DeploymentSpec
		err := json.Unmarshal(request.Body, &deployment)
		if err != nil {
			sLog.Infof("V (Solution): onReconcile failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		delete := request.Parameters["delete"]
		summary, err := c.SolutionManager.Reconcile(ctx, deployment, delete == "true", scope)
		data, _ := json.Marshal(summary)
		if err != nil {
			sLog.Infof("V (Solution): onReconcile failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  data,
			})
		}
		return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State:       v1alpha2.OK,
			Body:        data,
			ContentType: "application/json",
		})
	}
	sLog.Infof("V (Solution): onReconcile failed - 405 method not allowed, traceId: %s", span.SpanContext().TraceID().String())
	return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	})
}

func (c *SolutionVendor) onApplyDeployment(request v1alpha2.COARequest) v1alpha2.COAResponse {
	_, span := observability.StartSpan("Solution Vendor", request.Context, &map[string]string{
		"method": "onApplyDeployment",
	})
	defer span.End()

	sLog.Infof("V (Solution): onApplyDeployment %s, traceId: %s", request.Method, span.SpanContext().TraceID().String())
	scope, exist := request.Parameters["scope"]
	if !exist {
		scope = "default"
	}
	switch request.Method {
	case fasthttp.MethodPost:
		ctx, span := observability.StartSpan("Apply Deployment", request.Context, nil)
		defer span.End()
		deployment := new(model.DeploymentSpec)
		err := json.Unmarshal(request.Body, &deployment)
		if err != nil {
			sLog.Infof("V (Solution): onApplyDeployment failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
			return v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			}
		}
		response := c.doDeploy(ctx, *deployment, scope)
		return observ_utils.CloseSpanWithCOAResponse(span, response)
	case fasthttp.MethodGet:
		ctx, span := observability.StartSpan("Get Components", request.Context, nil)
		defer span.End()
		deployment := new(model.DeploymentSpec)
		err := json.Unmarshal(request.Body, &deployment)
		if err != nil {
			sLog.Infof("V (Solution): onApplyDeployment failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
			return v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			}
		}
		response := c.doGet(ctx, *deployment)
		return observ_utils.CloseSpanWithCOAResponse(span, response)
	case fasthttp.MethodDelete:
		ctx, span := observability.StartSpan("Delete Components", request.Context, nil)
		defer span.End()
		var deployment model.DeploymentSpec
		err := json.Unmarshal(request.Body, &deployment)
		if err != nil {
			return v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			}
		}
		response := c.doRemove(ctx, deployment, scope)
		return observ_utils.CloseSpanWithCOAResponse(span, response)
	}
	sLog.Infof("V (Solution): onApplyDeployment failed - 405 method not allowed, traceId: %s", span.SpanContext().TraceID().String())
	resp := v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	}
	observ_utils.UpdateSpanStatusFromCOAResponse(span, resp)
	return resp
}

func (c *SolutionVendor) doGet(ctx context.Context, deployment model.DeploymentSpec) v1alpha2.COAResponse {
	ctx, span := observability.StartSpan("Solution Vendor", ctx, &map[string]string{
		"method": "doGet",
	})
	defer span.End()
	sLog.Infof("V (Solution): doGet, traceId: %s", span.SpanContext().TraceID().String())

	_, components, err := c.SolutionManager.Get(ctx, deployment)
	if err != nil {
		sLog.Infof("V (Solution): doGet failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
		response := v1alpha2.COAResponse{
			State: v1alpha2.InternalError,
			Body:  []byte(err.Error()),
		}
		observ_utils.UpdateSpanStatusFromCOAResponse(span, response)
		return response
	}
	data, _ := json.Marshal(components)
	response := v1alpha2.COAResponse{
		State:       v1alpha2.OK,
		Body:        data,
		ContentType: "application/json",
	}
	observ_utils.UpdateSpanStatusFromCOAResponse(span, response)
	return response
}
func (c *SolutionVendor) doDeploy(ctx context.Context, deployment model.DeploymentSpec, scope string) v1alpha2.COAResponse {
	ctx, span := observability.StartSpan("Solution Vendor", ctx, &map[string]string{
		"method": "doDeploy",
	})
	defer span.End()
	sLog.Infof("V (Solution): doDeploy, traceId: %s", span.SpanContext().TraceID().String())
	summary, err := c.SolutionManager.Reconcile(ctx, deployment, false, scope)
	data, _ := json.Marshal(summary)
	if err != nil {
		sLog.Infof("V (Solution): doDeploy failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
		response := v1alpha2.COAResponse{
			State: v1alpha2.InternalError,
			Body:  data,
		}
		observ_utils.UpdateSpanStatusFromCOAResponse(span, response)
		return response
	}
	response := v1alpha2.COAResponse{
		State:       v1alpha2.OK,
		Body:        data,
		ContentType: "application/json",
	}
	observ_utils.UpdateSpanStatusFromCOAResponse(span, response)
	return response
}
func (c *SolutionVendor) doRemove(ctx context.Context, deployment model.DeploymentSpec, scope string) v1alpha2.COAResponse {
	ctx, span := observability.StartSpan("Solution Vendor", ctx, &map[string]string{
		"method": "doRemove",
	})
	defer span.End()

	sLog.Infof("V (Solution): doRemove, traceId: %s", span.SpanContext().TraceID().String())
	summary, err := c.SolutionManager.Reconcile(ctx, deployment, true, scope)
	data, _ := json.Marshal(summary)
	if err != nil {
		sLog.Infof("V (Solution): doRemove failed - %s, traceId: %s", err.Error(), span.SpanContext().TraceID().String())
		response := v1alpha2.COAResponse{
			State: v1alpha2.InternalError,
			Body:  data,
		}
		observ_utils.UpdateSpanStatusFromCOAResponse(span, response)
		return response
	}
	response := v1alpha2.COAResponse{
		State:       v1alpha2.OK,
		Body:        data,
		ContentType: "application/json",
	}
	observ_utils.UpdateSpanStatusFromCOAResponse(span, response)
	return response
}
