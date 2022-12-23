package logic

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gravitl/netmaker/database"
	"github.com/gravitl/netmaker/logger"
	"github.com/gravitl/netmaker/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrHostExists error indicating that host exists when trying to create new host
	ErrHostExists error = errors.New("host already exists")
	// ErrInvalidHostID
	ErrInvalidHostID error = errors.New("invalid host id")
)

// GetAllHosts - returns all hosts in flat list or error
func GetAllHosts() ([]models.Host, error) {
	currHostMap, err := GetHostsMap()
	if err != nil {
		return nil, err
	}
	var currentHosts = []models.Host{}
	for k := range currHostMap {
		var h = *currHostMap[k]
		currentHosts = append(currentHosts, h)
	}

	return currentHosts, nil
}

// GetAllHostsAPI - get's all the hosts in an API usable format
func GetAllHostsAPI(hosts []models.Host) []models.ApiHost {
	apiHosts := []models.ApiHost{}
	for i := range hosts {
		newApiHost := hosts[i].ConvertNMHostToAPI()
		apiHosts = append(apiHosts, *newApiHost)
	}
	return apiHosts[:]
}

// GetHostsMap - gets all the current hosts on machine in a map
func GetHostsMap() (map[string]*models.Host, error) {
	records, err := database.FetchRecords(database.HOSTS_TABLE_NAME)
	if err != nil && !database.IsEmptyRecord(err) {
		return nil, err
	}
	currHostMap := make(map[string]*models.Host)
	for k := range records {
		var h models.Host
		err = json.Unmarshal([]byte(records[k]), &h)
		if err != nil {
			return nil, err
		}
		currHostMap[h.ID.String()] = &h
	}

	return currHostMap, nil
}

// GetHost - gets a host from db given id
func GetHost(hostid string) (*models.Host, error) {
	record, err := database.FetchRecord(database.HOSTS_TABLE_NAME, hostid)
	if err != nil {
		return nil, err
	}

	var h models.Host
	if err = json.Unmarshal([]byte(record), &h); err != nil {
		return nil, err
	}

	return &h, nil
}

// CreateHost - creates a host if not exist
func CreateHost(h *models.Host) error {
	_, err := GetHost(h.ID.String())
	if (err != nil && !database.IsEmptyRecord(err)) || (err == nil) {
		return ErrHostExists
	}
	//encrypt that password so we never see it
	hash, err := bcrypt.GenerateFromPassword([]byte(h.HostPass), 5)
	if err != nil {
		return err
	}
	h.HostPass = string(hash)
	return UpsertHost(h)
}

// UpdateHost - updates host data by field
func UpdateHost(newHost, currentHost *models.Host) {
	// unchangeable fields via API here
	newHost.DaemonInstalled = currentHost.DaemonInstalled
	newHost.OS = currentHost.OS
	newHost.IPForwarding = currentHost.IPForwarding
	newHost.HostPass = currentHost.HostPass
	newHost.MacAddress = currentHost.MacAddress
	newHost.Debug = currentHost.Debug
	newHost.Nodes = currentHost.Nodes
	newHost.PublicKey = currentHost.PublicKey
	newHost.InternetGateway = currentHost.InternetGateway
	newHost.TrafficKeyPublic = currentHost.TrafficKeyPublic

	// changeable fields
	if len(newHost.Version) == 0 {
		newHost.Version = currentHost.Version
	}

	if len(newHost.Name) == 0 {
		newHost.Name = currentHost.Name
	}

	if newHost.LocalAddress.String() != currentHost.LocalAddress.String() {
		newHost.LocalAddress = currentHost.LocalAddress
	}

	if newHost.LocalRange.String() != currentHost.LocalRange.String() {
		newHost.LocalRange = currentHost.LocalRange
	}

	if newHost.MTU == 0 {
		newHost.MTU = currentHost.MTU
	}

	if newHost.ListenPort == 0 {
		newHost.ListenPort = currentHost.ListenPort
	}

	if newHost.ProxyListenPort == 0 {
		newHost.ProxyListenPort = currentHost.ProxyListenPort
	}
}

// UpsertHost - upserts into DB a given host model, does not check for existence*
func UpsertHost(h *models.Host) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}

	return database.Insert(h.ID.String(), string(data), database.HOSTS_TABLE_NAME)
}

// RemoveHost - removes a given host from server
func RemoveHost(h *models.Host) error {
	if len(h.Nodes) > 0 {
		for i := range h.Nodes {
			id := h.Nodes[i]
			n, err := GetNodeByID(id)
			if err == nil {
				if err = DeleteNodeByID(&n); err != nil {
					return err // must remove associated nodes before removing a host
				}
			}
		}
	}
	return database.DeleteRecord(database.HOSTS_TABLE_NAME, h.ID.String())
}

// UpdateHostNetworks - updates a given host's networks
func UpdateHostNetworks(h *models.Host, nets []string) error {
	if len(h.Nodes) > 0 {
		for i := range h.Nodes {
			n, err := GetNodeByID(h.Nodes[i])
			if err != nil {
				return err
			}
			// loop through networks and remove need for updating existing networks
			found := false
			for j := range nets {
				if len(nets[j]) > 0 && nets[j] == n.Network {
					nets[j] = "" // mark as ignore
					found = true
				}
			}
			if !found { // remove the node/host from that network
				if err = DeleteNodeByID(&n); err != nil {
					return err
				}
			}
		}
	} else {
		h.Nodes = []string{}
	}

	for i := range nets {
		// create a node for each non zero network remaining
		if len(nets[i]) > 0 {
			// TODO create a node with given hostid
			logger.Log(0, "I will create a node here")
		}
	}

	return nil
}

// AssociateNodeToHost - associates and creates a node with a given host
func AssociateNodeToHost(n *models.Node, h *models.Host) error {
	if len(h.ID.String()) == 0 || h.ID == uuid.Nil {
		return ErrInvalidHostID
	}
	n.HostID = h.ID
	err := CreateNode(n)
	if err != nil {
		return err
	}
	h.Nodes = append(h.Nodes, n.ID.String())
	return UpsertHost(h)
}

// DissasociateNodeFromHost - deletes a node and removes from host nodes
func DissasociateNodeFromHost(n *models.Node, h *models.Host) error {
	if len(h.ID.String()) == 0 || h.ID == uuid.Nil {
		return ErrInvalidHostID
	}
	if n.HostID != h.ID { // check if node actually belongs to host
		return fmt.Errorf("node is not associated with host")
	}
	if len(h.Nodes) == 0 {
		return fmt.Errorf("no nodes present in given host")
	}
	index := -1
	for i := range h.Nodes {
		if h.Nodes[i] == n.ID.String() {
			index = i
			break
		}
	}
	if index < 0 {
		if len(h.Nodes) == 0 {
			return fmt.Errorf("node %s, not found in host, %s", n.ID.String(), h.ID.String())
		}
	}
	h.Nodes = RemoveStringSlice(h.Nodes, index)
	return UpsertHost(h)
}
