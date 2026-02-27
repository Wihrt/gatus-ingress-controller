variable "REGISTRY" {
  default = "ghcr.io"
}

variable "IMAGE_NAME" {
  default = ""
}

variable "TAG" {
  default = "latest"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile"
  tags       = ["${REGISTRY}/${IMAGE_NAME}:${TAG}"]
}
