provider "google" {
  credentials = "${file("zfs-provisioner-ci.json")}"
  project     = "zfs-provisioner"
  region      = "europe-west"
  zone        = "europe-west3"
}

data "google_container_engine_versions" "west3a" {
  location       = "europe-west3-a"
  version_prefix = "1.13."
}

resource "google_container_cluster" "zfs-provisioner-ci" {
  name                     = "zfs-provisioner-ci"
  location                 = "europe-west3-a"
  initial_node_count       = 1
  remove_default_node_pool = true
  min_master_version       = "${data.google_container_engine_versions.west3a.latest_node_version}"

  # Setting an empty username and password explicitly disables basic auth
  master_auth {
    username = ""
    password = ""

    # client_certificate_config {
    #   issue_client_certificate = true
    # }
  }

  timeouts {
    create = "30m"
    update = "40m"
  }
}

resource "google_container_node_pool" "workers" {
  name       = "ci-nodes"
  location   = "europe-west3-a"
  cluster    = "${google_container_cluster.zfs-provisioner-ci.name}"
  node_count = 1
  version    = "${data.google_container_engine_versions.west3a.latest_node_version}"

  node_config {
    preemptible  = true
    machine_type = "g1-small"

    metadata = {
      disable-legacy-endpoints = "true"
    }
  }
}

resource "google_compute_instance" "fileserver" {
  name         = "zfs-provisioner-ci-fileserver"
  zone         = "europe-west3-a"
  machine_type = "g1-small"

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-1804-lts"
    }
  }


  network_interface {
    subnetwork = "${google_container_cluster.zfs-provisioner-ci.subnetwork}"
    access_config {
    }
  }
}

resource "google_service_account" "zfs-provisioner" {
  account_id = "zfs-provisioner"
  display_name = "zfs-provisioner"
}

resource "google_project_iam_binding" "zfs-provisioner" {
  role    = "roles/container.developer"
  members = ["serviceAccount:${google_service_account.zfs-provisioner.email}"]
}
