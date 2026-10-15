// Package components exposes the Cubeship services on a machine, independently
// of user workloads. The fixed inventory is also the allowlist for log reads.
package components

import "cubeship/internal/platform/bootstrap"

type Component struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Container     string `json:"container"`
	Image         string `json:"image,omitempty"`
	Status        string `json:"status"`
	Health        string `json:"health,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	Restarts      int    `json:"restarts"`
	LogsAvailable bool   `json:"logs_available"`
	Detail        string `json:"detail,omitempty"`
}

type Inventory struct {
	Server     string      `json:"server"`
	Components []Component `json:"components"`
}

var definitions = []Component{
	{ID: "daemon", Name: "Cubeship daemon", Container: bootstrap.DaemonContainerName, Description: "API, orchestration and cluster coordination."},
	{ID: "dashboard", Name: "Dashboard", Container: bootstrap.FrontendContainerName, Description: "The Cubeship web interface."},
	{ID: "traefik", Name: "Traefik", Container: bootstrap.TraefikContainerName, Description: "Ingress, TLS and routing to applications."},
	{ID: "postgres", Name: "Instance database", Container: bootstrap.PostgresContainerName, Description: "Cubeship identities, configuration and history. May use external Postgres."},
	{ID: "registry", Name: "Image registry", Container: bootstrap.RegistryContainerName, Description: "The instance's private container images."},
	{ID: "buildkit", Name: "BuildKit", Container: bootstrap.BuildKitContainerName, Description: "Builds application images. Started on demand."},
}
