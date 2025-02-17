/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 * SPDX-License-Identifier: MIT
 */

package models

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/managers"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states"

	observability "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/observability"
	observ_utils "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/observability/utils"
)

type ModelsManager struct {
	managers.Manager
	StateProvider states.IStateProvider
}

func (s *ModelsManager) Init(context *contexts.VendorContext, config managers.ManagerConfig, providers map[string]providers.IProvider) error {
	stateprovider, err := managers.GetStateProvider(config, providers)
	if err == nil {
		s.StateProvider = stateprovider
	} else {
		return err
	}
	return nil
}

func (t *ModelsManager) DeleteSpec(ctx context.Context, name string) error {
	ctx, span := observability.StartSpan("Models Manager", ctx, &map[string]string{
		"method": "DeleteSpec",
	})
	var err error = nil
	defer observ_utils.CloseSpanWithError(span, &err)

	err = t.StateProvider.Delete(ctx, states.DeleteRequest{
		ID: name,
		Metadata: map[string]string{
			"scope":    "",
			"group":    model.AIGroup,
			"version":  "v1",
			"resource": "models",
		},
	})
	return err
}

func (t *ModelsManager) UpsertSpec(ctx context.Context, name string, spec model.DeviceSpec) error {
	ctx, span := observability.StartSpan("Models Manager", ctx, &map[string]string{
		"method": "UpsertSpec",
	})
	var err error = nil
	defer observ_utils.CloseSpanWithError(span, &err)

	upsertRequest := states.UpsertRequest{
		Value: states.StateEntry{
			ID: name,
			Body: map[string]interface{}{
				"apiVersion": model.AIGroup + "/v1",
				"kind":       "model",
				"metadata": map[string]interface{}{
					"name": name,
				},
				"spec": spec,
			},
		},
		Metadata: map[string]string{
			"template": fmt.Sprintf(`{"apiVersion": "%s/v1", "kind": "Model", "metadata": {"name": "${{$model()}}"}}`, model.AIGroup),
			"scope":    "",
			"group":    model.AIGroup,
			"version":  "v1",
			"resource": "models",
		},
	}
	_, err = t.StateProvider.Upsert(ctx, upsertRequest)
	if err != nil {
		return err
	}
	return nil
}

func (t *ModelsManager) ListSpec(ctx context.Context) ([]model.ModelState, error) {
	ctx, span := observability.StartSpan("Models Manager", ctx, &map[string]string{
		"method": "ListSpec",
	})
	var err error = nil
	defer observ_utils.CloseSpanWithError(span, &err)

	listRequest := states.ListRequest{
		Metadata: map[string]string{
			"version":  "v1",
			"group":    model.AIGroup,
			"resource": "models",
		},
	}
	models, _, err := t.StateProvider.List(ctx, listRequest)
	if err != nil {
		return nil, err
	}
	ret := make([]model.ModelState, 0)
	for _, t := range models {
		var rt model.ModelState
		rt, err = getModelState(t.ID, t.Body, t.ETag)
		if err != nil {
			return nil, err
		}
		ret = append(ret, rt)
	}
	return ret, nil
}

func getModelState(id string, body interface{}, etag string) (model.ModelState, error) {
	dict := body.(map[string]interface{})
	spec := dict["spec"]

	j, _ := json.Marshal(spec)
	var rSpec model.ModelSpec
	err := json.Unmarshal(j, &rSpec)
	if err != nil {
		return model.ModelState{}, err
	}
	//rSpec.Generation??
	state := model.ModelState{
		Id:   id,
		Spec: &rSpec,
	}
	return state, nil
}

func (t *ModelsManager) GetSpec(ctx context.Context, id string) (model.ModelState, error) {
	ctx, span := observability.StartSpan("Models Manager", ctx, &map[string]string{
		"method": "GetSpec",
	})
	var err error = nil
	defer observ_utils.CloseSpanWithError(span, &err)

	getRequest := states.GetRequest{
		ID: id,
		Metadata: map[string]string{
			"version":  "v1",
			"group":    model.AIGroup,
			"resource": "models",
		},
	}
	m, err := t.StateProvider.Get(ctx, getRequest)
	if err != nil {
		return model.ModelState{}, err
	}

	ret, err := getModelState(id, m.Body, m.ETag)
	if err != nil {
		return model.ModelState{}, err
	}
	return ret, nil
}
