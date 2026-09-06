# Image Recognition & OCR Architecture

## Overview

The Alliance Manager utilizes a highly optimized, decoupled image processing pipeline. Heavy optical character recognition and image manipulation are offloaded to a dedicated Python microservice (**`lastwar-ocr-service`**) hosted on Google Cloud Run. This architecture allows users to drag-and-drop up to 100 screenshots simultaneously, while the Go backend securely brokers the data and resolves player aliases with near-perfect accuracy.

## The Smart Extraction Pipeline

The system processes image uploads through a sequential, intelligent pipeline divided between the Go backend and the external microservice to minimize API costs and maximize data accuracy.

### Step 1: Secure Microservice Invocation (Go Backend)
```go
ProcessImagesViaWorker(ctx, files, workerURL) → CVWorkerResponse
```
When an admin uploads a batch of screenshots, the Go server decrypts the AES-GCM encrypted Google Service Account JSON key from its database. It uses this key to generate a secure OIDC identity token and POSTs the raw images to the protected Python microservice endpoint.

### Step 2: OpenCV UI Categorization (External Microservice)
Instead of relying on the user to sort their uploads, the external worker mathematically analyzes the UI geometry of every screenshot:
* **Band-Pass Masking:** It masks out everything except the 10%–25% height region of the screen, isolating the main navigation tabs and ignoring in-game scrolling marquees or yellow player rows.
* **Geometric Filtering & Width Ratios:** It locates the active orange tab and measures its bounding box. If the tab takes up less than 42% of the screen width (a 3-tab layout), it categorizes the image as "Power Rankings". If it is ~50% (a 2-tab layout), it categorizes it as "VS Points".
* **Sub-Tab Detection:** For VS Points, it applies a binary threshold below the orange header to find the active white weekday tab, routing the image to the correct day bucket.

### Step 3: Dynamic Vertical Stitching 
Google Cloud Vision API charges per image. To drastically reduce API quota usage:
* The microservice iterates through each categorized bucket (e.g., all "tuesday" images) and stitches them vertically into a single massive image tower.
* This allows the system to send 1 image payload to GCP instead of 10, while maintaining razor-sharp text resolution.

### Step 4: Spatial Parsing Engine
Because standard OCR reads text left-to-right and top-to-bottom, vertical table layouts often get mangled. The worker uses a specialized spatial geometry engine to extract data:
1. **Score Anchoring:** It searches the returned bounding boxes for valid numeric scores (Values >= 10,000).
2. **Row-Band Clustering:** For every score found, it creates a virtual horizontal "band" to grab every word that falls within that Y-axis band to the left of the score.
3. **Gap Detection:** It reconstructs the player's name by measuring the X-axis pixel gaps between the bounding boxes, intelligently preserving spaces in names like "Dumptruck 911" while merging fractured words.

### Step 5: Surgical Regex Truncation
Before returning the data to the Go backend, the worker cleans the raw strings using Regular Expressions:
* Instantly truncates Alliance Tags by splitting the string at the first bracket (`[`).
* Strips leading list numbers (e.g., "16 ").
* Aggressively removes alliance badges (`R1`-`R5`) and single-character OCR hallucinations (e.g., a drop-shadow misread as a `B` or `N`).

### Step 6: Alias Resolution & Validation UI (Go Backend)
The microservice returns a clean JSON payload mapping categories to their extracted records. The Go server receives this and runs the names through its tiered **Alias Engine** (`Exact Name -> Personal Alias -> Global Alias -> OCR Alias`).
* The data is *not* saved immediately. Instead, a categorized JSON payload (Matched, Unresolved, Ambiguous) is returned to the frontend "Preview & Confirm" modal.
* **OCR Aliases:** When an admin manually maps a mangled name in the UI, they can save it as an `ocr` alias. This machine-read correction is explicitly hidden from standard roster searches but trains the Alias Engine to automatically correct that specific visual artifact in all future uploads.

## Advantages of the Microservice Architecture

### Before (Monolithic Go Pipeline) ❌
* Required heavy C++ OCR libraries or strict white-pixel logic that broke on varying phone aspect ratios.
* Go's strict typing made spatial geometry math and image matrix manipulation incredibly verbose and difficult to maintain.
* A crash in the OCR pipeline could potentially panic the entire web server.

### After (Python Cloud Run Worker) ✅
* **Decoupled:** The Go backend remains incredibly lightweight. The OCR worker scales automatically on Cloud Run and scales to zero when not in use.
* **Robust Math:** Python handles matrix math, HSV color isolation, and standard deviation confidence guards effortlessly.
* **Cost-Effective:** Vertical stitching combined with precise categorization cuts GCP Vision API costs by over 80%.

## Technical Requirements

* **External Service:** The `lastwar-ocr-service` must be deployed to Google Cloud Run (or a similar container host).
* **Admin Configuration:** The Cloud Run endpoint URL must be saved in the Alliance Manager's Admin Settings dashboard.
* **Secure Credentials:** A Google Cloud Service Account JSON key with `Cloud Run Invoker` permission (to invoke the private OCR worker) must be uploaded to the Go backend. If you enable **OCR request archival to GCS** (below), the *same* service account additionally needs `Storage Object Creator` on the archive bucket — roles are additive and the extra grant does not affect OCR auth.

> **Deploying the OCR service / Cloud Run instance** is documented in the [`lastwar-ocr-service`](https://github.com/shodiwarmic/lastwar-ocr-service) repository — follow its setup guide for the Cloud Run deployment, Vision API enablement, and service-account creation. This repo intentionally does not duplicate those steps.

## OCR Request Archival (optional)

The Alliance Manager can retain a best-effort copy of each OCR request — the
uploaded screenshots plus the parsed response — to help improve OCR accuracy and
diagnose extraction mistakes. It is **off by default**, never blocks or affects the
user's OCR result, and is chosen in **Admin → Security → OCR Request Archival**
(`none` / Google Cloud Storage / local disk / both). It works for either OCR
backend (cloud or local).

### Local-disk archival
Set `OCR_ARCHIVE_DIR` (a path inside a mounted volume — the default
`/app/data/ocr-archive` lives under the existing `./data` mount) and select
**local** (or **both**) in the admin UI. Archives are auto-pruned after
`OCR_ARCHIVE_RETENTION_DAYS` (default 7) by an in-app janitor. See `DEPLOYMENT.md`
for the volume/retention operational notes.

### GCS archival setup
Reuses the same `gcp_vision` service account already used to invoke Cloud Run. One
bucket, with a write-only grant (least privilege) and a server-side retention
rule:

```bash
# 1. Create the bucket (same region as your Cloud Run service keeps data local)
gcloud storage buckets create gs://lastwar-ocr-archive \
  --project=<your-project> --location=<region> --uniform-bucket-level-access

# 2. Add a 14-day auto-delete lifecycle rule (retention happens server-side)
cat > /tmp/lifecycle.json <<'EOF'
{ "lifecycle": { "rule": [ { "action": {"type":"Delete"}, "condition": {"age":14} } ] } }
EOF
gcloud storage buckets update gs://lastwar-ocr-archive --lifecycle-file=/tmp/lifecycle.json

# 3. Grant the service account WRITE-ONLY access (cannot read/list/delete)
gcloud storage buckets add-iam-policy-binding gs://lastwar-ocr-archive \
  --member="serviceAccount:<your-sa>@<project>.iam.gserviceaccount.com" \
  --role="roles/storage.objectCreator"
```

Then, in **Admin → Security → OCR Request Archival**, enter the bucket name and
select **Google Cloud Storage** (or **both**). With the write-only `objectCreator`
role, a smoke-test *delete* failing is the correct confirmation that least
privilege is in effect.