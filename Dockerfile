# Copyright © 2018 VMware, Inc. All Rights Reserved.
# SPDX-License-Identifier: MIT

FROM vmware/photon
ADD dist/k8s-endpoints-sync-controller /k8s-endpoints-sync-controller
RUN chmod +x /k8s-endpoints-sync-controller
CMD "/k8s-endpoints-sync-controller"
