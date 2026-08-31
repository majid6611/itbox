-- Compute Mesh (Power Management): a friendly name plus the MeshCentral
-- node id it maps to. AMT credentials are never stored here — they live
-- only inside MeshCentral's own data (see modules/compute-mesh), which
-- nothing but this backend ever reaches.
CREATE TABLE compute_mesh_devices (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    node_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
