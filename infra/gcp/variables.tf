# NOTE ON CI: this module's `terraform plan`/`apply` require real GCP
# credentials (the google provider validates against the live API even
# during planning), so CI only runs `fmt -check` and `validate` — both
# fully offline. Running `plan`/`apply` for real is a deliberate, local,
# credentialed action — see the main README for instructions.
variable "project_id" {
  description = "GCP project ID to deploy into."
  type        = string
}

variable "region" {
  description = "GCP region for the VMs and network."
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone for the VMs."
  type        = string
  default     = "us-central1-a"
}

variable "machine_type" {
  description = "Compute Engine machine type for both VMs."
  type        = string
  default     = "e2-small"
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key file authorized to log in as the 'deploy' user. Generate one with `make lab-keys` or `ssh-keygen -t ed25519 -f infra-key`."
  type        = string
}

variable "ssh_source_ranges" {
  description = "CIDR ranges allowed to reach the VMs over SSH (port 22). Defaults to open; strongly recommended to restrict this to your own IP before applying."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "app_port" {
  description = "Port the sample app listens on."
  type        = number
  default     = 8080
}

variable "name_prefix" {
  description = "Prefix applied to all created resource names, to avoid collisions if this module is applied more than once in the same project."
  type        = string
  default     = "canopy"
}
