# Wordsmith App

Wordsmith is the demo project originally shown at DockerCon EU 2017 and 2018.

The demo app runs across three containers:

- **[api](api/Dockerfile)** - a Java REST API which serves words read from the database
- **[web](web/Dockerfile)** - a Go web application that calls the API and builds words into sentences
- **db** - a Postgres database that stores words

## Architecture

![Architecture diagram](architecture.excalidraw.png)

## Build and run in Docker Compose

The only requirement to build and run the app from source is Docker. Clone this repo and use Docker Compose to build all the images. You can use the new V2 Compose with `docker compose` or the classic `docker-compose` CLI:

```shell
docker compose up --build
```

Or you can pull pre-built images from Docker Hub using `docker compose pull`.

---

## Deployment & Monitoring Guide

This guide details how to deploy the Wordsmith application to Kubernetes, configure Dynatrace monitoring (including OneAgent and OpenTelemetry), and set up Chaos Mesh to run reliability experiments.

### 1. Deploying the Wordsmith Application

You can deploy the app to Kubernetes using the provided [Kustomize configuration](./kustomization.yaml). 

First, create a dedicated namespace (optional but recommended):
```shell
kubectl create namespace wordsmith
```

Apply the manifests (if deploying to the `wordsmith` namespace, pass the `-n` flag):
```shell
kubectl apply -k . -n wordsmith
```

Verify that the pods are running:
```shell
kubectl get pods -n wordsmith
```

You should see:
- 1 pod for `db`
- 1 pod for `web`
- 5 replica pods for `api`

To access the web interface, find the external IP / Port of the `web` service:
```shell
kubectl get svc -n wordsmith
```
*Note: If you are running locally (e.g., Docker Desktop), browse to http://localhost:8080.*

---

### 2. Setting Up Dynatrace Monitoring

Using the **Dynatrace OneAgent** alongside OpenTelemetry is highly recommended, especially when testing with Chaos Mesh.

#### Why Use Dynatrace OneAgent for Chaos Mesh Testing?
When Chaos Mesh injects a **Pod Fault** (such as pod failure or container kill) or a **Network Fault** (such as packet loss or latency):
1. **OpenTelemetry Limitations**: Since OpenTelemetry instrumentation runs directly inside the application process:
   - If the pod is killed, the application process terminates, and it cannot emit metrics or trace data.
   - If a network fault is injected, the application's OTLP exporter may not be able to reach the telemetry collector (`telemetry-ingest.dynatrace.svc.cluster.local:4317`), causing metric loss.
2. **OneAgent Advantages**: 
   - OneAgent runs as a node-level DaemonSet (`cloudNativeFullStack`). Even if the pod is killed or network routes inside the pod are blocked, OneAgent continues to report container restarts, crash-loops, host resource utilization, and Kubernetes state events.
   - OneAgent automatically injects instrumentation into containers annotated with `oneagent.dynatrace.com/inject: "true"` (already defined in `api.yaml` and `web.yaml`), capturing database calls, incoming requests, and dependencies with zero code modification.

#### How to Download the DynaKube YAML
1. Log in to your Dynatrace Environment.
2. Navigate to **Manage** > **Kubernetes** or click **Deploy Dynatrace** > **Start Installation** > **Kubernetes**.
3. Provide a name for your Kubernetes cluster (e.g., `k8s-cluster`).
4. Click **Generate Token** to create the API and Data Ingest tokens.
5. Under the installation commands, you will find a link to download the customized configuration YAML (`dynakube.yaml`) or a `curl` command to download it directly.
6. Alternatively, customize the pre-configured [dynakube.yaml](./dynakube.yaml) file in this repository with your Dynatrace API URL (`apiUrl`) and Secret tokens.

#### Deploying the Dynatrace Agent (Operator & DynaKube)
1. Install the Dynatrace Operator via Helm:
   ```shell
   helm repo add dynatrace https://raw.githubusercontent.com/Dynatrace/dynatrace-operator/master/artifact/helm/repo
   helm repo update
   helm install dynatrace-operator dynatrace/dynatrace-operator -n dynatrace --create-namespace --atomic
   ```
2. Apply the `dynakube.yaml` configuration to start the OneAgent DaemonSet and ActiveGate:
   ```shell
   kubectl apply -f dynakube.yaml
   ```
3. Check the status of the Dynatrace monitoring pods:
   ```shell
   kubectl get pods -n dynatrace
   ```

---

### 3. Deploying Chaos Mesh

Chaos Mesh is a cloud-native Chaos Engineering platform that orchestrates chaos on Kubernetes environments.

#### Deploying Chaos Mesh using Helm
1. Add the Chaos Mesh Helm repository:
   ```shell
   helm repo add chaos-mesh https://charts.chaos-mesh.org
   helm repo update
   ```
2. Deploy Chaos Mesh using the custom values file provided in this repository (which configures resources and adds OneAgent injection annotations to Chaos Mesh components so that Dynatrace monitors the chaos controller itself):
   ```shell
   helm install chaos-mesh chaos-mesh/chaos-mesh \
     -n chaos-mesh \
     --create-namespace \
     -f k8s-manifests/chaos-mesh-values.yaml
   ```
3. Verify that the Chaos Mesh pods are running:
   ```shell
   kubectl get pods -n chaos-mesh
   ```

#### Accessing the Chaos Dashboard
To access the Chaos Mesh dashboard locally, port-forward the dashboard service:
```shell
kubectl port-forward -n chaos-mesh svc/chaos-dashboard 2333:2333
```
Now, open your browser and navigate to http://localhost:2333. You can use this dashboard or raw YAML manifests to define experiments (e.g., PodChaos, NetworkChaos).
