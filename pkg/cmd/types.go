/*
Copyright 2022 The KubeVela Authors.

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

package cmd

const (
	flagNamespace = "namespace"
	flagEnv       = "env"
	flagCluster   = "cluster"
	flagGroup     = "group"
)

const (
	usageNamespace = "If present, the namespace scope for this CLI request"
	usageEnv       = "The environment name for the CLI request"
	usageCluster   = "The cluster to execute the current command"
	usageGroup     = "The resource group (e.g. apps) of the resource kind"
)
