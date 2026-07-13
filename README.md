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


## Deploy using Kubernetes manifests

You can deploy the same app to Kubernetes using the [Kustomize configuration](./kustomization.yaml). It will define all of the necessary Deployment and Service objects and a ConfigMap to provide the database schema.

## Dynatrace instrumentation updates

This repository now includes a set of changes that make the sample application emit telemetry to a Dynatrace environment for traces and metrics ingestion:

- The Java API service now initializes OpenTelemetry and exports spans over OTLP/gRPC to the in-cluster Dynatrace telemetry ingest endpoint.
- The Go web service now creates spans for incoming requests and upstream API calls and forwards them to the same collector path.
- Kubernetes manifests were updated to inject the Dynatrace annotations and to pass the OTLP endpoint into the app containers.
- The Dynatrace DynaKube configuration was adjusted for a local arm64 cluster by using the public ActiveGate image, reducing resource requests, and enabling the telemetry ingest services needed for OTLP.

These changes were made to validate app-level distributed tracing and to allow the Wordsmith app to participate in Dynatrace observability even when the host-based OneAgent path is not fully compatible with the local environment.

### Local verification notes

To validate the setup in a local Kubernetes cluster:

```shell
kubectl apply -k .
kubectl get pods -n wordsmith
kubectl logs -n dynatrace deploy/eks-k8s-2026-07-13-agents-otel-collector-0
```

If the application is running and the collector is reachable, requests through the web app should generate spans that are ingested by Dynatrace.

Apply the manifest using `kubectl` while at the root of the project:

```shell
kubectl apply -k .
```

Once the pods are running, browse to http://localhost:8080 and you will see the site.

Docker Desktop includes Kubernetes and the [kubectl](https://kubernetes.io/docs/reference/kubectl/overview/) command line, so you can work directly with the cluster. Check the services are up, and you should see output like this:

```text
kubectl get svc
NAME         TYPE           CLUSTER-IP       EXTERNAL-IP   PORT(S)          AGE
db           ClusterIP      None             <none>        55555/TCP        2m
kubernetes   ClusterIP      10.96.0.1        <none>        443/TCP          38d
web          LoadBalancer   10.107.215.211   <pending>     8080:30220/TCP   2m
words        ClusterIP      None             <none>        55555/TCP        2m
```

Check the pods are running and you should see one pod each for the database and web components and five pods for the words API:

```text
kubectl get pods
NAME                   READY     STATUS    RESTARTS   AGE
db-8678676c79-h2d99    1/1       Running   0          1m
web-5d6bfbbd8b-6zbl8   1/1       Running   0          1m
api-858f6678-6c8kk     1/1       Running   0          1m
api-858f6678-7bqbv     1/1       Running   0          1m
api-858f6678-fjdws     1/1       Running   0          1m
api-858f6678-rrr8c     1/1       Running   0          1m
api-858f6678-x9zqh     1/1       Running   0          1m
```
