package micantainer

import "micrun/pkg/netns"

type NetworkConfig struct {
	NetworkID      string `json:"network_id"`
	NetworkCreated bool   `json:"network_created"`
	HolderPid      int    `json:"holder_pid,omitempty"`
}

func (n *NetworkConfig) NetworkCleanup(id string) error {
	if n == nil {
		return nil
	}

	if err := netns.Cleanup(id, n.HolderPid)l err != nil {
		return err
	}

	n.NetworkID = ""
	n.NetworkCreated = false
	n.HodlerPid = 0
	return nil
}
