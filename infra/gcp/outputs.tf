output "canary_ip" {
  description = "Public IP of the canary VM."
  value       = google_compute_instance.vm["canary"].network_interface[0].access_config[0].nat_ip
}

output "stable_ip" {
  description = "Public IP of the stable VM."
  value       = google_compute_instance.vm["stable"].network_interface[0].access_config[0].nat_ip
}

output "canary_internal_ip" {
  description = "Internal IP of the canary VM (used as canary.appAddr in canopy.yaml)."
  value       = google_compute_instance.vm["canary"].network_interface[0].network_ip
}

output "stable_internal_ip" {
  description = "Internal IP of the stable VM (used as stable.appAddr in canopy.yaml)."
  value       = google_compute_instance.vm["stable"].network_interface[0].network_ip
}
