// Copyright © 2018 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"time"
)

type Config struct {
	ClustersToWatch     []string
	ClusterToApply      string
	NamespaceToWatch    string
	NamespacesToExclude []string
	ReplicatedLabelVal  string
	WatchNamespaces     bool
	WatchEndpoints      bool
	WatchServices       bool
	ResyncPeriod        time.Duration
}

const REPLICATED_LABEL_KEY = "replicated"
const KUBERNETES = "kubernetes"
const SVC_ANNOTATION_SYNDICATE_KEY = "vmware.com/syndicate-mode"
const SVC_ANNOTATION_UNION = "union"
const SVC_ANNOTATION_SOURCE = "source"
const SVC_ANNOTATION_RECEIVER = "receiver"
const SVC_ANNOTATION_SINGULAR = "singular"
