resource "google_compute_network" "canopy" {
  name                    = "${var.name_prefix}-network"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "canopy" {
  name          = "${var.name_prefix}-subnet"
  ip_cidr_range = "10.10.0.0/24"
  region        = var.region
  network       = google_compute_network.canopy.id
}

# SSH access, restricted to var.ssh_source_ranges (default: open — see the
# warning on that variable). This is the only inbound path canopy itself
# needs; all deploy/traffic-shift operations happen over this one port.
resource "google_compute_firewall" "allow_ssh" {
  name    = "${var.name_prefix}-allow-ssh"
  network = google_compute_network.canopy.id

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = var.ssh_source_ranges
  target_tags   = ["${var.name_prefix}-vm"]
}

# App traffic, open to the internet by design — this is the service
# canopy is progressively rolling out, meant to be publicly reachable.
resource "google_compute_firewall" "allow_app" {
  name    = "${var.name_prefix}-allow-app"
  network = google_compute_network.canopy.id

  allow {
    protocol = "tcp"
    ports    = [tostring(var.app_port)]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["${var.name_prefix}-vm"]
}

# Internal traffic between the two VMs (health checks, service-to-service
# calls in a more realistic app) — restricted to the subnet itself.
resource "google_compute_firewall" "allow_internal" {
  name    = "${var.name_prefix}-allow-internal"
  network = google_compute_network.canopy.id

  allow {
    protocol = "tcp"
    ports    = ["0-65535"]
  }

  source_ranges = [google_compute_subnetwork.canopy.ip_cidr_range]
  target_tags   = ["${var.name_prefix}-vm"]
}
