locals {
  vms = {
    canary = "canary"
    stable = "stable"
  }
}

resource "google_compute_instance" "vm" {
  for_each = local.vms

  name         = "${var.name_prefix}-${each.value}"
  machine_type = var.machine_type
  zone         = var.zone
  tags         = ["${var.name_prefix}-vm"]

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 20
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.canopy.id
    access_config {} # ephemeral public IP
  }

  metadata = {
    ssh-keys     = "deploy:${file(var.ssh_public_key_path)}"
    TARGET_LABEL = each.value
    startup-script = templatefile("${path.module}/scripts/startup.sh", {
      TARGET_LABEL = each.value
    })
  }

  labels = {
    role    = each.value
    managed = "canopy"
  }
}
