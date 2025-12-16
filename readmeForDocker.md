# Running the DFS with Docker (multiple clients)

This document shows a short workflow to run the DFS cluster (master + 3 chunkservers) and exercise multiple clients using Docker.

Assumptions
- You are on Linux and inside the project root: `dfs-project`
- Docker and Docker Compose are installed and working
- You have a Makefile with the usual targets (`docker-build`, `docker-up`, `docker-down`, `docker-client-*`)

Quick checklist (first run)
```bash
# from project root
# 1) build docker images and create helper files (WAL/checkpoint)
make docker-build

# 2) bring the cluster up (master + 3 chunkservers)
make docker-up
```

```bash
# 3) Create client folders to simulate multiple clients
./setup_clients.sh 3   # creates clients/client1..client3 directories but can create as many directories as one want
```

Running multiple clients (examples)
The Makefile provides convenient wrappers that run the `client` binary inside a temporary container and mount your project directory into `/workspace` so uploads/downloads interact with host files.

- Upload a file as client 1:
```bash
make docker-client-upload CLIENT=1 FILE=test.pdf
```

- List files for client 2:
```bash
make docker-client-ls CLIENT=2
```

- Download a file as client 3:
```bash
make docker-client-download CLIENT=3 FILE=test.pdf
```

- Delete a file as client 1:
```bash
make docker-client-delete CLIENT=1 FILE=test.pdf
```

Cleaning up
```bash
# stop and remove containers, networks, volumes
make docker-clean
# or
docker-compose down -v
```

To see status of chunkservers and master:
```bash
docker-compose ps
```

If something breaks
- Check master logs first: `docker logs dfs-master` or `tail -n 200 log_files/master.log`.
- If master fails to start because of an invalid `master.checkpoint` (invalid JSON), remove or replace it with valid JSON. The master treats missing checkpoint as "start from WAL" and will recover.

