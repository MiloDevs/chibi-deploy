# ChibiDeploy
--- 

Chibi deploy is a simple command line utility which allows you to build, push and deploy images to your servers, largely aimed at web applications.

### Prerequisites

- Golang tooling - [install go](https://go.dev/doc/install "documentation for installing the go toolchain")

---

### Getting started 

```sh
git clone https://github.com/MiloDevs/chibi-deploy
```

```sh
cd chibi-deploy
```

```sh
GOBIN=/path/to/bin go install
```

Now you can run chibi-deploy anywhere from your system, recommended location for GOBIN is `/home/your/user/bin` for isolation and remember to add this folder to `PATH`

---

### Usage

```sh
chibi-deploy init
```

creates the basic yaml and secrets file for you in the current directory


```sh
chibi-deploy deploy
```

runs the deploy script step by step:
  - first it builds your images
  - pushes built images to your choses registry, *currently only supports ghcr*
  - ssh into server and executes your deploy script.

---

.secrets file expected format - This will be more adjustable in the future

```
SSH_USER=
SSH_HOST=
SSH_KEY=
GHCR_USER=
GH_TOKEN=
```

---

### Warning

This is still a work in progress and very rudimentary but it works, I'll continue to improve on this as time goes on but feel free to open any issues or feature requests. Thank you!
