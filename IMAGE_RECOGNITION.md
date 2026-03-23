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
* **Secure Credentials:** A Google Cloud Service Account JSON key with `Cloud Vision API` and `Cloud Run Invoker` permissions must be uploaded to the Go backend.