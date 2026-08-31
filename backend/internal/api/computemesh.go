package api

import (
	"context"
	"log"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/meshcentral"
)

// bootstrapComputeMesh runs once, right after compute-mesh's container
// starts. MeshCentral needs a device group to hold every registered
// device — there's no config-time way to create one (unlike the account
// itself, which the module's own init container creates before the
// server ever starts, so there's no NetBird-style REST bootstrap dance
// needed for that part). Polls because the server generates its own TLS
// certs on first boot, which takes a few seconds.
func (s *Server) bootstrapComputeMesh(ctx context.Context) {
	deadline := time.Now().Add(2 * time.Minute)
	var config map[string]string
	for time.Now().Before(deadline) {
		st, ok, err := s.Modules.GetInstalled(ctx, "compute-mesh")
		if err == nil && ok && st.Status == "running" {
			config = st.Config
			break
		}
		time.Sleep(3 * time.Second)
	}
	if config == nil {
		log.Printf("compute-mesh bootstrap: module never reached running, giving up")
		return
	}

	addr := s.Modules.ServiceAddr("compute-mesh", "meshcentral", 443)
	client := meshcentral.NewClient(addr, "api", config["api_password"])

	var meshID string
	for time.Now().Before(deadline) {
		id, err := client.CreateMesh(ctx, "Devices")
		if err == nil {
			meshID = id
			break
		}
		time.Sleep(3 * time.Second)
	}
	if meshID == "" {
		log.Printf("compute-mesh bootstrap: create device group never succeeded, giving up")
		return
	}

	_, err := s.DB.Exec(ctx,
		`UPDATE installed_modules SET config = config || jsonb_build_object('mesh_id', $2::text) WHERE module_id = $1`,
		"compute-mesh", meshID)
	if err != nil {
		log.Printf("compute-mesh bootstrap: persist mesh id: %v", err)
		return
	}
	log.Printf("compute-mesh bootstrap: complete")
}

// computeMeshClient returns a ready-to-use client and the device group
// id if compute-mesh is installed, running, and has finished its one-time
// bootstrap (device group id present) — available=false otherwise, e.g.
// "still bootstrapping" right after install.
func (s *Server) computeMeshClient(ctx context.Context) (client *meshcentral.Client, meshID string, available bool, err error) {
	status, ok, err := s.Modules.GetInstalled(ctx, "compute-mesh")
	if err != nil {
		return nil, "", false, err
	}
	if !ok || status.Status != "running" {
		return nil, "", false, nil
	}
	meshID = status.Config["mesh_id"]
	if meshID == "" || status.Config["api_password"] == "" {
		return nil, "", false, nil
	}
	addr := s.Modules.ServiceAddr("compute-mesh", "meshcentral", 443)
	client = meshcentral.NewClient(addr, "api", status.Config["api_password"])
	return client, meshID, true, nil
}

type ListMeshDevicesInput struct {
	SessionToken string `cookie:"itp_session"`
}

type MeshDeviceOut struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ListMeshDevicesOutput struct {
	Body struct {
		Available bool            `json:"available"`
		Devices   []MeshDeviceOut `json:"devices"`
	}
}

type AddMeshDeviceInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Name        string `json:"name"`
		Host        string `json:"host"`
		AMTUsername string `json:"amt_username"`
		AMTPassword string `json:"amt_password"`
	}
}

type RemoveMeshDeviceInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           int    `path:"id"`
}

type MeshDevicePowerInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           int    `path:"id"`
	Body         struct {
		// "on", "off", or "cycle" — Intel AMT power control, works even
		// with a crashed/hung OS since it's independent of it.
		Action string `json:"action"`
	}
}

func registerComputeMesh(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-mesh-devices",
		Method:      "GET",
		Path:        "/api/compute-mesh/devices",
		Summary:     "List registered computers",
	}, func(ctx context.Context, in *ListMeshDevicesInput) (*ListMeshDevicesOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &ListMeshDevicesOutput{}
		_, _, available, err := s.computeMeshClient(ctx)
		if err != nil {
			return nil, internalError("check compute mesh module", err)
		}
		out.Body.Available = available
		out.Body.Devices = []MeshDeviceOut{}
		if !available {
			return out, nil
		}
		rows, err := s.DB.Query(ctx, `SELECT id, name FROM compute_mesh_devices ORDER BY name`)
		if err != nil {
			return nil, internalError("list devices", err)
		}
		defer rows.Close()
		for rows.Next() {
			var d MeshDeviceOut
			if err := rows.Scan(&d.ID, &d.Name); err != nil {
				return nil, internalError("scan device", err)
			}
			out.Body.Devices = append(out.Body.Devices, d)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "add-mesh-device",
		Method:      "POST",
		Path:        "/api/compute-mesh/devices",
		Summary:     "Register a computer for remote power control (Intel AMT)",
	}, func(ctx context.Context, in *AddMeshDeviceInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, meshID, available, err := s.computeMeshClient(ctx)
		if err != nil {
			return nil, internalError("check compute mesh module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Compute Mesh module first (or it's still finishing setup — try again in a minute)")
		}
		if in.Body.Name == "" || in.Body.Host == "" || in.Body.AMTUsername == "" || in.Body.AMTPassword == "" {
			return nil, huma.Error400BadRequest("name, host, AMT username, and AMT password are all required")
		}
		if err := client.AddAMTDevice(ctx, meshID, in.Body.Name, in.Body.Host, in.Body.AMTUsername, in.Body.AMTPassword); err != nil {
			return nil, huma.Error400BadRequest("could not register device", err)
		}
		nodes, err := client.Nodes(ctx, meshID)
		if err != nil {
			return nil, internalError("look up registered device", err)
		}
		nodeID, ok := nodes[in.Body.Name]
		if !ok {
			return nil, internalError("registered device not found afterward", nil)
		}
		if _, err := s.DB.Exec(ctx, `INSERT INTO compute_mesh_devices (name, node_id) VALUES ($1, $2)`, in.Body.Name, nodeID); err != nil {
			return nil, internalError("save device", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "remove-mesh-device",
		Method:      "DELETE",
		Path:        "/api/compute-mesh/devices/{id}",
		Summary:     "Unregister a computer",
	}, func(ctx context.Context, in *RemoveMeshDeviceInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		nodeID, err := s.meshDeviceNodeID(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		client, _, available, err := s.computeMeshClient(ctx)
		if err != nil {
			return nil, internalError("check compute mesh module", err)
		}
		if available {
			if err := client.RemoveDevice(ctx, nodeID); err != nil {
				return nil, internalError("remove device from compute mesh", err)
			}
		}
		if _, err := s.DB.Exec(ctx, `DELETE FROM compute_mesh_devices WHERE id = $1`, in.ID); err != nil {
			return nil, internalError("delete device", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "mesh-device-power",
		Method:      "POST",
		Path:        "/api/compute-mesh/devices/{id}/power",
		Summary:     "Power a computer on, off, or cycle it via Intel AMT — works even if the OS has crashed",
	}, func(ctx context.Context, in *MeshDevicePowerInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		var actionType int
		switch in.Body.Action {
		case "on":
			actionType = meshcentral.ActionAMTPowerOn
		case "off":
			actionType = meshcentral.ActionAMTPowerOff
		case "cycle":
			actionType = meshcentral.ActionAMTPowerCycle
		default:
			return nil, huma.Error400BadRequest(`action must be "on", "off", or "cycle"`)
		}
		nodeID, err := s.meshDeviceNodeID(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		client, _, available, err := s.computeMeshClient(ctx)
		if err != nil {
			return nil, internalError("check compute mesh module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("the Compute Mesh module isn't available")
		}
		if err := client.PowerAction(ctx, []string{nodeID}, actionType); err != nil {
			return nil, huma.Error500InternalServerError("power action failed", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}

func (s *Server) meshDeviceNodeID(ctx context.Context, id int) (string, error) {
	var nodeID string
	err := s.DB.QueryRow(ctx, `SELECT node_id FROM compute_mesh_devices WHERE id = $1`, id).Scan(&nodeID)
	if err != nil {
		return "", huma.Error404NotFound("no such device")
	}
	return nodeID, nil
}
