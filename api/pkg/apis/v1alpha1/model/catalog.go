/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 * SPDX-License-Identifier: MIT
 */

package model

import (
	"errors"
	"reflect"
)

// TODO: all state objects should converge to this paradigm: id, spec and status
type CatalogState struct {
	Id     string         `json:"id"`
	Spec   *CatalogSpec   `json:"spec,omitempty"`
	Status *CatalogStatus `json:"status,omitempty"`
}

// +kubebuilder:object:generate=true
type ObjectRef struct {
	SiteId     string            `json:"siteId"`
	Name       string            `json:"name"`
	Group      string            `json:"group"`
	Version    string            `json:"version"`
	Kind       string            `json:"kind"`
	Scope      string            `json:"scope"`
	Address    string            `json:"address,omitempty"`
	Generation string            `json:"generation,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}
type CatalogSpec struct {
	SiteId     string                 `json:"siteId"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Metadata   map[string]string      `json:"metadata,omitempty"`
	ParentName string                 `json:"parentName,omitempty"`
	ObjectRef  ObjectRef              `json:"objectRef,omitempty"`
	Generation string                 `json:"generation,omitempty"`
}

type CatalogStatus struct {
	Properties map[string]string `json:"properties"`
}

func (c CatalogSpec) DeepEquals(other IDeepEquals) (bool, error) {
	otherC, ok := other.(CatalogSpec)
	if !ok {
		return false, errors.New("parameter is not a CatalogSpec type")
	}

	if c.SiteId != otherC.SiteId {
		return false, nil
	}

	if c.Name != otherC.Name {
		return false, nil
	}

	if c.ParentName != otherC.ParentName {
		return false, nil
	}

	if c.Generation != otherC.Generation {
		return false, nil
	}

	if !reflect.DeepEqual(c.Properties, otherC.Properties) {
		return false, nil
	}

	return true, nil
}

// INode interface
func (s CatalogState) GetId() string {
	return s.Id
}
func (s CatalogState) GetParent() string {
	if s.Spec != nil {
		return s.Spec.ParentName
	}
	return ""
}
func (s CatalogState) GetType() string {
	if s.Spec != nil {
		return s.Spec.Type
	}
	return ""
}
func (s CatalogState) GetProperties() map[string]interface{} {
	if s.Spec != nil {
		return s.Spec.Properties
	}
	return nil
}

// IEdge interface
func (s CatalogState) GetFrom() string {
	if s.Spec != nil {
		if s.Spec.Type == "edge" {
			if s.Spec.Metadata != nil {
				if from, ok := s.Spec.Metadata["from"]; ok {
					return from
				}
			}
		}
	}
	return ""
}

func (s CatalogState) GetTo() string {
	if s.Spec != nil {
		if s.Spec.Type == "edge" {
			if s.Spec.Metadata != nil {
				if to, ok := s.Spec.Metadata["to"]; ok {
					return to
				}
			}
		}
	}
	return ""
}
