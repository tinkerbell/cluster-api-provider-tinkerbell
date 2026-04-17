/*
Copyright 2022 The Tinkerbell Authors.

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

package v1beta2

// Hub marks TinkerbellCluster as the hub (storage) version for conversion.
func (*TinkerbellCluster) Hub() {}

// Hub marks TinkerbellClusterList as the hub (storage) version for conversion.
func (*TinkerbellClusterList) Hub() {}

// Hub marks TinkerbellMachine as the hub (storage) version for conversion.
func (*TinkerbellMachine) Hub() {}

// Hub marks TinkerbellMachineList as the hub (storage) version for conversion.
func (*TinkerbellMachineList) Hub() {}

// Hub marks TinkerbellMachineTemplate as the hub (storage) version for conversion.
func (*TinkerbellMachineTemplate) Hub() {}

// Hub marks TinkerbellMachineTemplateList as the hub (storage) version for conversion.
func (*TinkerbellMachineTemplateList) Hub() {}
