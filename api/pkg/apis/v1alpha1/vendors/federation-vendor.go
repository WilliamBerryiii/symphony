/*
   MIT License

   Copyright (c) Microsoft Corporation.

   Permission is hereby granted, free of charge, to any person obtaining a copy
   of this software and associated documentation files (the "Software"), to deal
   in the Software without restriction, including without limitation the rights
   to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
   copies of the Software, and to permit persons to whom the Software is
   furnished to do so, subject to the following conditions:

   The above copyright notice and this permission notice shall be included in all
   copies or substantial portions of the Software.

   THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
   IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
   FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
   AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
   LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
   OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
   SOFTWARE

*/

package vendors

import (
	"context"
	"encoding/json"

	"github.com/azure/symphony/api/pkg/apis/v1alpha1/managers/catalogs"
	"github.com/azure/symphony/api/pkg/apis/v1alpha1/managers/sites"
	"github.com/azure/symphony/api/pkg/apis/v1alpha1/managers/staging"
	"github.com/azure/symphony/api/pkg/apis/v1alpha1/managers/sync"
	"github.com/azure/symphony/api/pkg/apis/v1alpha1/model"
	"github.com/azure/symphony/api/pkg/apis/v1alpha1/utils"
	"github.com/azure/symphony/coa/pkg/apis/v1alpha2"
	"github.com/azure/symphony/coa/pkg/apis/v1alpha2/managers"
	"github.com/azure/symphony/coa/pkg/apis/v1alpha2/observability"
	observ_utils "github.com/azure/symphony/coa/pkg/apis/v1alpha2/observability/utils"
	"github.com/azure/symphony/coa/pkg/apis/v1alpha2/providers"
	"github.com/azure/symphony/coa/pkg/apis/v1alpha2/providers/pubsub"
	"github.com/azure/symphony/coa/pkg/apis/v1alpha2/vendors"
	"github.com/azure/symphony/coa/pkg/logger"
	"github.com/valyala/fasthttp"
)

var fLog = logger.NewLogger("coa.runtime")

type FederationVendor struct {
	vendors.Vendor
	SitesManager    *sites.SitesManager
	CatalogsManager *catalogs.CatalogsManager
	StagingManager  *staging.StagingManager
	SyncManager     *sync.SyncManager
}

func (f *FederationVendor) GetInfo() vendors.VendorInfo {
	return vendors.VendorInfo{
		Version:  f.Vendor.Version,
		Name:     "Federation",
		Producer: "Microsoft",
	}
}
func (f *FederationVendor) Init(config vendors.VendorConfig, factories []managers.IManagerFactroy, providers map[string]map[string]providers.IProvider, pubsubProvider pubsub.IPubSubProvider) error {
	err := f.Vendor.Init(config, factories, providers, pubsubProvider)
	if err != nil {
		return err
	}
	for _, m := range f.Managers {
		if c, ok := m.(*sites.SitesManager); ok {
			f.SitesManager = c
		}
		if c, ok := m.(*staging.StagingManager); ok {
			f.StagingManager = c
		}
		if c, ok := m.(*catalogs.CatalogsManager); ok {
			f.CatalogsManager = c
		}
		if c, ok := m.(*sync.SyncManager); ok {
			f.SyncManager = c
		}
	}
	if f.StagingManager == nil {
		return v1alpha2.NewCOAError(nil, "staging manager is not supplied", v1alpha2.MissingConfig)
	}
	if f.SitesManager == nil {
		return v1alpha2.NewCOAError(nil, "sites manager is not supplied", v1alpha2.MissingConfig)
	}
	if f.CatalogsManager == nil {
		return v1alpha2.NewCOAError(nil, "catalogs manager is not supplied", v1alpha2.MissingConfig)
	}
	f.Vendor.Context.Subscribe("catalog", func(topic string, event v1alpha2.Event) error {
		sites, err := f.SitesManager.ListSpec(context.Background())
		if err != nil {
			return err
		}
		for _, site := range sites {
			event.Metadata["site"] = site.Spec.Name
			f.StagingManager.HandleJobEvent(context.Background(), event) //TODO: how to handle errors in this case?
		}
		return nil
	})
	f.Vendor.Context.Subscribe("remote", func(topic string, event v1alpha2.Event) error {
		_, ok := event.Metadata["site"]
		if !ok {
			return v1alpha2.NewCOAError(nil, "site is not supplied", v1alpha2.BadRequest)
		}
		f.StagingManager.HandleJobEvent(context.Background(), event) //TODO: how to handle errors in this case?
		return nil
	})
	f.Vendor.Context.Subscribe("report", func(topic string, event v1alpha2.Event) error {
		if status, ok := event.Body.(model.ActivationStatus); ok {
			err := utils.SyncActivationStatus("http://localhost:8082/v1alpha2/", "admin", "", status)
			if err != nil {
				return err
			}
		}
		return v1alpha2.NewCOAError(nil, "report is not an activation status", v1alpha2.BadRequest)
	})
	return nil
}
func (f *FederationVendor) GetEndpoints() []v1alpha2.Endpoint {
	route := "federation"
	if f.Route != "" {
		route = f.Route
	}
	return []v1alpha2.Endpoint{
		{
			Methods:    []string{fasthttp.MethodPost, fasthttp.MethodGet},
			Route:      route + "/sync",
			Version:    f.Version,
			Handler:    f.onSync,
			Parameters: []string{"site?"},
		},
		{
			Methods:    []string{fasthttp.MethodPost, fasthttp.MethodGet},
			Route:      route + "/registry",
			Version:    f.Version,
			Handler:    f.onRegistry,
			Parameters: []string{"name?"},
		},
		{
			Methods:    []string{fasthttp.MethodGet},
			Route:      route + "/graph",
			Version:    f.Version,
			Handler:    f.onGraph,
			Parameters: []string{"name?"},
		},
		{
			Methods: []string{fasthttp.MethodPost},
			Route:   route + "/trail",
			Version: f.Version,
			Handler: f.onTrail,
		},
	}
}
func (f *FederationVendor) onRegistry(request v1alpha2.COARequest) v1alpha2.COAResponse {
	pCtx, span := observability.StartSpan("Federation Vendor", request.Context, &map[string]string{
		"method": "onRegistry",
	})
	tLog.Info("V (Federation): onRegistry")
	switch request.Method {
	case fasthttp.MethodGet:
		ctx, span := observability.StartSpan("onRegistry-GET", pCtx, nil)
		id := request.Parameters["__name"]
		var err error
		var state interface{}
		isArray := false
		if id == "" {
			state, err = f.SitesManager.ListSpec(ctx)
			isArray = true
		} else {
			state, err = f.SitesManager.GetSpec(ctx, id)
		}
		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		jData, _ := utils.FormatObject(state, isArray, request.Parameters["path"], request.Parameters["doc-type"])
		resp := observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State:       v1alpha2.OK,
			Body:        jData,
			ContentType: "application/json",
		})
		if request.Parameters["doc-type"] == "yaml" {
			resp.ContentType = "application/text"
		}
		return resp
	case fasthttp.MethodPost:
		ctx, span := observability.StartSpan("onRegistry-POST", pCtx, nil)
		id := request.Parameters["__name"]

		var site model.SiteSpec
		err := json.Unmarshal(request.Body, &site)
		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		//TODO: generate site key pair as needed
		err = f.SitesManager.UpsertSpec(ctx, id, site)
		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State: v1alpha2.OK,
		})
	case fasthttp.MethodDelete:
		ctx, span := observability.StartSpan("onRegistry-DELETE", pCtx, nil)
		id := request.Parameters["__name"]
		err := f.SitesManager.DeleteSpec(ctx, id)
		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State: v1alpha2.OK,
		})
	}
	resp := v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	}
	observ_utils.UpdateSpanStatusFromCOAResponse(span, resp)
	span.End()
	return resp
}
func (f *FederationVendor) onSync(request v1alpha2.COARequest) v1alpha2.COAResponse {
	pCtx, span := observability.StartSpan("Federation Vendor", request.Context, &map[string]string{
		"method": "onSync",
	})
	tLog.Info("V (Federation): onSync")
	switch request.Method {
	case fasthttp.MethodPost:
		var status model.ActivationStatus
		err := json.Unmarshal(request.Body, &status)
		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.BadRequest,
				Body:  []byte(err.Error()),
			})
		}
		err = f.Vendor.Context.Publish("job-report", v1alpha2.Event{
			Body: status,
		})
		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State: v1alpha2.OK,
		})
	case fasthttp.MethodGet:
		ctx, span := observability.StartSpan("onSync-GET", pCtx, nil)
		id := request.Parameters["__site"]
		batch, err := f.StagingManager.GetABatchForSite(id)

		pack := model.SyncPackage{
			Origin: f.Context.Site,
		}

		if err != nil {
			return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
				State: v1alpha2.InternalError,
				Body:  []byte(err.Error()),
			})
		}
		catalogs := make([]model.CatalogSpec, 0)
		jobs := make([]v1alpha2.JobData, 0)
		for _, c := range batch {
			if c.Action == "RUN" { //TODO: I don't really like this
				jobs = append(jobs, c)
			} else {
				catalog, err := f.CatalogsManager.GetSpec(ctx, c.Id)
				if err != nil {
					return observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
						State: v1alpha2.InternalError,
						Body:  []byte(err.Error()),
					})
				}
				catalogs = append(catalogs, *catalog.Spec)
			}
		}
		pack.Catalogs = catalogs
		pack.Jobs = jobs
		jData, _ := utils.FormatObject(pack, true, request.Parameters["path"], request.Parameters["doc-type"])
		resp := observ_utils.CloseSpanWithCOAResponse(span, v1alpha2.COAResponse{
			State:       v1alpha2.OK,
			Body:        jData,
			ContentType: "application/json",
		})
		if request.Parameters["doc-type"] == "yaml" {
			resp.ContentType = "application/text"
		}
		return resp
	}
	resp := v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	}
	observ_utils.UpdateSpanStatusFromCOAResponse(span, resp)
	span.End()
	return resp
}
func (f *FederationVendor) onGraph(request v1alpha2.COARequest) v1alpha2.COAResponse {
	resp := v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	}
	return resp
}
func (f *FederationVendor) onTrail(request v1alpha2.COARequest) v1alpha2.COAResponse {
	resp := v1alpha2.COAResponse{
		State:       v1alpha2.MethodNotAllowed,
		Body:        []byte("{\"result\":\"405 - method not allowed\"}"),
		ContentType: "application/json",
	}
	return resp
}
