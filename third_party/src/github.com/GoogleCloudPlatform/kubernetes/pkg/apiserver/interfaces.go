/*
Copyright 2014 Google Inc. All rights reserved.

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

package apiserver

import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

// RESTStorage is a generic interface for RESTful storage services
// Resources which are exported to the RESTful API of apiserver need to implement this interface.
type RESTStorage interface {
	// New returns an empty object that can be used with Create and Update after request data has been put into it.
	// This object must be a pointer type for use with Codec.DecodeInto([]byte, interface{})
	New() interface{}

	// List selects resources in the storage which match to the selector.
	List(labels.Selector) (interface{}, error)

	// Get finds a resource in the storage by id and returns it.
	// Although it can return an arbitrary error value, IsNotFound(err) is true for the returned error value err when the specified resource is not found.
	Get(id string) (interface{}, error)

	// Delete finds a resource in the storage and deletes it.
	// Although it can return an arbitrary error value, IsNotFound(err) is true for the returned error value err when the specified resource is not found.
	Delete(id string) (<-chan interface{}, error)

	Create(interface{}) (<-chan interface{}, error)
	Update(interface{}) (<-chan interface{}, error)
}

// ResourceWatcher should be implemented by all RESTStorage objects that
// want to offer the ability to watch for changes through the watch api.
type ResourceWatcher interface {
	// TODO: take a query, like List, to filter out unwanted events.
	WatchAll() (watch.Interface, error)
	WatchSingle(id string) (watch.Interface, error)
}
