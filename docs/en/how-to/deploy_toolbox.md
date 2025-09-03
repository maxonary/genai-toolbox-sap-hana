---
title: "Deploy to Cloud Run"
type: docs
weight: 3
description: >
  How to set up and configure Toolbox to run on Cloud Run.
---


## Before you begin

1. [Install](https://cloud.google.com/sdk/docs/install) the Google Cloud CLI.

1. Set the PROJECT_ID environment variable:

    ```bash
    export PROJECT_ID="my-project-id"
    ```

1. Initialize gcloud CLI:

    ```bash
    gcloud init
    gcloud config set project $PROJECT_ID
    ```

1. Make sure you've set up and initialized your database.

1. You must have the following APIs enabled:

    ```bash
    gcloud services enable run.googleapis.com \
                           cloudbuild.googleapis.com \
                           artifactregistry.googleapis.com \
                           iam.googleapis.com \
                           secretmanager.googleapis.com

    ```

1. To create an IAM account, you must have the following IAM permissions (or
   roles):
    - Create Service Account role (roles/iam.serviceAccountCreator)

1. To create a secret, you must have the following roles:
    - Secret Manager Admin role (roles/secretmanager.admin)

1. To deploy to Cloud Run, you must have the following set of roles:
    - Cloud Run Developer (roles/run.developer)
    - Service Account User role (roles/iam.serviceAccountUser)

{{< notice note >}}
If you are using sources that require VPC-access (such as
AlloyDB or Cloud SQL over private IP), make sure your Cloud Run service and the
database are in the same VPC network.
{{< /notice >}}

## Create a service account

1. Create a backend service account if you don't already have one:

    ```bash
    gcloud iam service-accounts create toolbox-identity
    ```

1. Grant permissions to use secret manager:

    ```bash
    gcloud projects add-iam-policy-binding $PROJECT_ID \
        --member serviceAccount:toolbox-identity@$PROJECT_ID.iam.gserviceaccount.com \
        --role roles/secretmanager.secretAccessor
    ```

1. Grant additional permissions to the service account that are specific to the
   source, e.g.:
    - [AlloyDB for PostgreSQL](../resources/sources/alloydb-pg.md#iam-permissions)
    - [Cloud SQL for PostgreSQL](../resources/sources/cloud-sql-pg.md#iam-permissions)

## Configure `tools.yaml` file

Create a `tools.yaml` file that contains your configuration for Toolbox. For
details, see the
[configuration](../resources/sources/)
section.

## Deploy to Cloud Run

1. Upload `tools.yaml` as a secret:

    ```bash
    gcloud secrets create tools --data-file=tools.yaml
    ```

    If you already have a secret and want to update the secret version, execute
    the following:

    ```bash
    gcloud secrets versions add tools --data-file=tools.yaml
    ```

1. Set an environment variable to the container image that you want to use for
   cloud run:

    ```bash
    export IMAGE=us-central1-docker.pkg.dev/database-toolbox/toolbox/toolbox:latest
    ```
{{< notice note >}}  
**The `$PORT` Environment Variable**  
Google Cloud Run dictates the port your application must listen on by setting the
`$PORT` environment variable inside your container. This value defaults to
**8080**. Your application's `--port` argument **must** be set to listen on this
port. If there is a mismatch, the container will fail to start and the
deployment will time out.  
{{< /notice >}}

1. Deploy Toolbox to Cloud Run using the following command:

    ```bash
    gcloud run deploy toolbox \
        --image $IMAGE \
        --service-account toolbox-identity \
        --region us-central1 \
        --set-secrets "/app/tools.yaml=tools:latest" \
        --args="--tools-file=/app/tools.yaml","--address=0.0.0.0","--port=8080"
        # --allow-unauthenticated # https://cloud.google.com/run/docs/authenticating/public#gcloud
    ```

    If you are using a VPC network, use the command below:

    ```bash
    gcloud run deploy toolbox \
        --image $IMAGE \
        --service-account toolbox-identity \
        --region us-central1 \
        --set-secrets "/app/tools.yaml=tools:latest" \
        --args="--tools-file=/app/tools.yaml","--address=0.0.0.0","--port=8080" \
        # TODO(dev): update the following to match your VPC if necessary
        --network default \
        --subnet default
        # --allow-unauthenticated # https://cloud.google.com/run/docs/authenticating/public#gcloud
    ```

## Connecting with Toolbox Client SDK

You can connect to Toolbox Cloud Run instances directly through the SDK.

1. [Set up `Cloud Run Invoker` role
   access](https://cloud.google.com/run/docs/securing/managing-access#service-add-principals)
   to your Cloud Run service.

1. (Only for local runs) Set up [Application Default
   Credentials](https://cloud.google.com/docs/authentication/set-up-adc-local-dev-environment)
   for the principal you set up the `Cloud Run Invoker` role access to.

1. Run the following to retrieve a non-deterministic URL for the cloud run service:

    ```bash
    gcloud run services describe toolbox --format 'value(status.url)'
    ```

1. Import and initialize the toolbox client with the URL retrieved above:

    {{< tabpane persist=header >}}
{{< tab header="Python" lang="python" >}}
from toolbox_core import ToolboxClient, auth_methods

# Replace with the Cloud Run service URL generated in the previous step.
URL = "https://cloud-run-url.app"

auth_token_provider = auth_methods.aget_google_id_token(URL) # can also use sync method

async with ToolboxClient(
    URL,
    client_headers={"Authorization": auth_token_provider},
) as toolbox:
{{< /tab >}}
{{< tab header="Javascript" lang="javascript" >}}
import { ToolboxClient } from '@toolbox-sdk/core';
import {getGoogleIdToken} from '@toolbox-sdk/core/auth'

// Replace with the Cloud Run service URL generated in the previous step.
const URL = 'http://127.0.0.1:5000';
const authTokenProvider = () => getGoogleIdToken(URL);

const client = new ToolboxClient(URL, null, {"Authorization": authTokenProvider});
{{< /tab >}}
{{< tab header="Go" lang="go" >}}
import "github.com/googleapis/mcp-toolbox-sdk-go/core"

func main() {
    // Replace with the Cloud Run service URL generated in the previous step.
    URL := "http://127.0.0.1:5000"
    auth_token_provider, err := core.GetGoogleIDToken(ctx, URL)
    if err != nil {
        log.Fatalf("Failed to fetch token %v", err)
    }
    toolboxClient, err := core.NewToolboxClient(
        URL,
        core.WithClientHeaderString("Authorization", auth_token_provider))
    if err != nil {
        log.Fatalf("Failed to create Toolbox client: %v", err)
    }
}
{{< /tab >}}
{{< /tabpane >}}


Now, you can use this client to connect to the deployed Cloud Run instance!

## Troubleshooting

{{< notice note >}}  
For any deployment or runtime error, the best first step is to check the logs for your service in the Google Cloud Console's Cloud Run section. They often contain the specific error message needed to diagnose the problem.  
{{< /notice >}}

* **Deployment Fails with "Container failed to start":** This is almost always
    caused by a port mismatch. Ensure your container's `--port` argument is set to
    `8080` to match the `$PORT` environment variable provided by Cloud Run.

* **Client Receives Permission Denied Error (401 or 403):** If your client application (e.g., your local SDK) gets a `401 Unauthorized` or `403 Forbidden` error when trying to call your Cloud Run service, it means the client is not properly authenticated as an invoker.
    * Ensure the user or service account calling the service has the **Cloud Run Invoker** (`roles/run.invoker`) IAM role.
    * If running locally, make sure your Application Default Credentials are set up correctly by running `gcloud auth application-default login`.

* **Service Fails to Access Secrets (in logs):** If your application starts but the logs show errors like "permission denied" when trying to access Secret Manager, it means the Toolbox service account is missing permissions.
    * Ensure the `toolbox-identity` service account has the **Secret Manager Secret Accessor** (`roles/secretmanager.secretAccessor`) IAM role.


