// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mutate

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type ConfigMutation func(v1.ConfigFile) (*v1.ConfigFile, error)

type configMutator struct {
	base     v1.Image
	mutation ConfigMutation

	computed   bool
	configFile *v1.ConfigFile
	manifest   *v1.Manifest
}

var _ v1.Image = (*configMutator)(nil)

func (i *configMutator) MediaType() (types.MediaType, error) { return i.base.MediaType() }

// Layers returns the ordered collection of filesystem layers that comprise this image.
// The order of the list is oldest/base layer first, and most-recent/top layer last.
func (i *configMutator) Layers() ([]v1.Layer, error) {
	return i.base.Layers()
}

// BlobSet returns an unordered collection of all the blobs in the image.
func (i *configMutator) BlobSet() (map[v1.Hash]struct{}, error) {
	return i.base.BlobSet()
}

// ConfigName returns the hash of the image's config file.
func (i *configMutator) ConfigName() (v1.Hash, error) {
	return partial.ConfigName(i)
}

// ConfigFile returns this image's config file.
func (i *configMutator) ConfigFile() (*v1.ConfigFile, error) {
	if err := i.compute(); err != nil {
		return nil, err
	}
	return i.configFile, nil
}

// RawConfigFile returns the serialized bytes of ConfigFile()
func (i *configMutator) RawConfigFile() ([]byte, error) {
	if err := i.compute(); err != nil {
		return nil, err
	}
	return json.Marshal(i.configFile)
}

// Digest returns the sha256 of this image's manifest.
func (i *configMutator) Digest() (v1.Hash, error) {
	return partial.Digest(i)
}

// Manifest returns this image's Manifest object.
func (i *configMutator) Manifest() (*v1.Manifest, error) {
	if err := i.compute(); err != nil {
		return nil, err
	}
	return i.manifest, nil
}

// RawManifest returns the serialized bytes of Manifest()
func (i *configMutator) RawManifest() ([]byte, error) {
	if err := i.compute(); err != nil {
		return nil, err
	}
	return json.Marshal(i.manifest)
}

// LayerByDigest returns a Layer for interacting with a particular layer of
// the image, looking it up by "digest" (the compressed hash).
func (i *configMutator) LayerByDigest(h v1.Hash) (v1.Layer, error) {
	return i.base.LayerByDigest(h)
}

// LayerByDiffID is an analog to LayerByDigest, looking up by "diff id"
// (the uncompressed hash).
func (i *configMutator) LayerByDiffID(h v1.Hash) (v1.Layer, error) {
	return i.base.LayerByDiffID(h)
}

func (i *configMutator) compute() error {
	// Don't re-compute if already computed.
	if i.computed {
		return nil
	}
	cf, err := i.base.ConfigFile()
	if err != nil {
		return err
	}
	configFile := cf.DeepCopy()
	configFile, err = i.mutation(*configFile)
	if err != nil {
		return err
	}

	i.configFile = configFile

	// Update manifest, too.
	m, err := i.base.Manifest()
	if err != nil {
		return err
	}
	i.manifest = m.DeepCopy()

	i.computed = true

	rcfg, err := i.RawConfigFile()
	if err != nil {
		i.computed = false
		return err
	}
	d, sz, err := v1.SHA256(bytes.NewBuffer(rcfg))
	if err != nil {
		i.computed = false
		return err
	}
	i.manifest.Config.Digest = d
	i.manifest.Config.Size = sz

	return nil
}

// Config mutates the provided v1.Image to have the provided v1.Config
func Config(base v1.Image, cfg v1.Config) (v1.Image, error) {
	setConfig := func(cf v1.ConfigFile) (*v1.ConfigFile, error) {
		cf.Config = cfg
		return &cf, nil
	}
	return &configMutator{
		base:     base,
		mutation: setConfig,
	}, nil
}

// MutateConfig mutates the provided v1.Image via the provided ConfigMutation.
func MutateConfig(base v1.Image, mut ConfigMutation) (v1.Image, error) {
	return &configMutator{
		base:     base,
		mutation: mut,
	}, nil
}

// CreatedAt mutates the provided v1.Image to have the provided v1.Time
func CreatedAt(base v1.Image, created v1.Time) (v1.Image, error) {
	setCreatedAt := func(cf v1.ConfigFile) (*v1.ConfigFile, error) {
		cf.Created = created
		return &cf, nil
	}

	return &configMutator{
		base:     base,
		mutation: setCreatedAt,
	}, nil
}

// Time sets all timestamps in an image to the given timestamp.
func Time(img v1.Image, t time.Time) (v1.Image, error) {
	setConfigTime := func(cf v1.ConfigFile) (*v1.ConfigFile, error) {
		// Strip away timestamps from the config file
		cf.Created = v1.Time{Time: t}

		for _, h := range cf.History {
			h.Created = v1.Time{Time: t}
		}

		return &cf, nil
	}

	return &configMutator{
		base:     img,
		mutation: setConfigTime,
	}, nil
}

// Canonical is a helper function to combine Time and configFile
// to remove any randomness during a docker build.
func Canonical(img v1.Image) (v1.Image, error) {
	// Set all timestamps to 0
	created := time.Time{}
	img, err := Time(img, created)
	if err != nil {
		return nil, err
	}

	stripNondeterminism := func(cf v1.ConfigFile) (*v1.ConfigFile, error) {
		cf.Container = ""
		cf.Config.Hostname = ""
		cf.ContainerConfig.Hostname = ""
		cf.DockerVersion = ""

		return &cf, nil
	}

	return &configMutator{
		base:     img,
		mutation: stripNondeterminism,
	}, nil
}
