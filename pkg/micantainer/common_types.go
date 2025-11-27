package micantainer

// ContainerType is a string representing the type of a container.
type ContainerType string

// Defines the different types of containers.
const (
	// PodContainer identifies a container that should be associated with an existing pod.
	PodContainer ContainerType = "pod_container"
	// PodSandbox identifies an infra container that will be used to create a pod.
	PodSandbox ContainerType = "pod_sandbox"
	// SideCar identifies a sidecar container.
	SideCar ContainerType = "side_car"
	// SingleContainer is utilized to describe a container that doesn't have a container/sandbox
	// annotation applied. This is expected when dealing with non-pod containers (e.g., from ctr, podman).
	SingleContainer ContainerType = "single_container"
	// UnknownContainerType specifies a container that provides a container type annotation, but it is unknown.
	UnknownContainerType ContainerType = "unknown_container_type"
)
