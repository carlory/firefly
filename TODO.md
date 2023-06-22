# TODO 

1. 搭建环境

[x] use kind to setup a kubernetes as host cluster
[x] deploy a kubernetes controlplane on host cluster, copy logic from kubeadm


# Note

1. The node status reporting is depend on the firefly-controller-manager, if the firefly-controller-manager is not running, the node status will not be updated and the node will be marked as not ready by the kube-controller-manager. It will cause the kube-controller-manager to stop the pod running on the node. It is unacceptable. So we need to find a way to make the node status reporting independent of the firefly-controller-manager. Pobosible solutions:
    1.1 Copy the node-lifecycle controller from the kube-controller-manager to the firefly-controller-manager. When fireflyadm deploy firefly, the node-lifecycle controller will be enabled in firefly-controller-manager and disabled in kube-controller-manager.
    1.2 Running a daemon in all the cluster nodes and report the node status to the kube-apiserver directly. The daemon will be deployed by the firefly.
