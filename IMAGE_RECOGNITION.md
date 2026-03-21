# Image Recognition & OCR Architecture

## Overview

The Alliance Manager utilizes a highly optimized, multi-threaded image processing pipeline powered by **Google Cloud Vision Document AI**. This architecture allows users to drag-and-drop up to 100 screenshots of varying types simultaneously, automatically sorting, compiling, and extracting data with near-perfect accuracy.

## The Smart Extraction Pipeline

The system processes image uploads through a sequential, intelligent pipeline designed to minimize API costs while maximizing data accuracy.

### Step 1: Smart Bucketing (Tab Detection)
```go
detectDayFromTabRegion(imageData) → string
```
Instead of manually selecting the data type, the system analyzes the top 11% to 22% of every uploaded image. It scans specifically for pure white pixels (`RGB > 240`) across 6 horizontal column zones.
* **Match Found:** The image is bucketed into a specific VS Points day (e.g., "monday", "wednesday").
* **No Match:** The image is routed to the "unknown" bucket (processed as Power Rankings).

### Step 2: Dynamic Vertical Stitching (Mega-pixel Chunking)
```go
stitchImagesVertically(imageDatas) → []byte
```
Google Cloud Vision has strict megapixel and aspect ratio limits. To prevent text compression and blurring:
* The system iterates through each bucket and stacks the screenshots vertically into a single image.
* **Dynamic Limits:** The system reads the image headers (without loading them fully into memory) and chunks the stitches so no single payload exceeds **12,000 pixels in height** or **15 images**.
* This drastically reduces API quota usage (sending 1 image instead of 10) while maintaining razor-sharp text resolution.

### Step 3: Cloud Vision API Extraction
```go
getGCPClient(ctx) → *vision.ImageAnnotatorClient
```
The server decrypts the AES-GCM encrypted Google Service Account JSON key in memory, establishes a secure gRPC connection to Google Cloud, and submits the stitched image. GCP returns a complete text representation of the image, natively reading it as a structured document/spreadsheet.

### Step 4: Hybrid State Machine Parsing
```go
parseVSPointsText(text) / parsePowerRankingsText(text) → []SmartRecord
```
Because GCP Document AI often reads data vertically (straight down a column) rather than horizontally, traditional regex fails. The system uses a specialized state machine:
1.  **Junk Filtering:** Explicitly ignores UI text ("Commander", "Points", "Ranking").
2.  **Number Routing:** If it sees a massive number (e.g., `45000000`), it tags it as a Value. If it sees a small number (e.g., `2`), it assumes it's a Rank Badge and ignores it.
3.  **Entity Pairing:** It remembers the last valid String (Player Name) it saw and pairs it with the next valid Value it encounters.

### Step 5: Alias Resolution & Validation UI
The extracted records are aggregated and run through the backend Alias Engine. The system attempts to resolve each scanned name using a strict hierarchy: `Exact Name -> Personal Alias -> Global Alias -> OCR Alias`. 
* If a match fails, the system falls back to a Levenshtein-like similarity algorithm (70% threshold for VS Points, 50% for Power).
* The data is *not* saved immediately. Instead, a categorized JSON payload (Matched vs. Unresolved) is returned to the frontend "Preview & Confirm" modal.

### Step 6: Database Commit & OCR Training
Administrators review the proposed data in the UI. For any "Unresolved" records, the admin can manually map the scanned string to an existing member.
* **OCR Aliases:** When an admin maps a mangled name (e.g., "M@rk" instead of "Mark"), they can save it as an `ocr` alias. This machine-read correction is strictly utilized by the background ingestion engine and explicitly hidden from standard roster searches, improving future automated imports without polluting the UI.
* All verified records and new alias mappings are finally saved to the database inside a single, secure SQLite transaction.

## Advantages of the GCP Architecture

### Before (Tesseract Local OCR) ❌
* Required heavy C++ libraries (`libtesseract-dev`) on the host server.
* Required fragile, manual image preprocessing (grayscale, contrast, inversion, cropping) to make text readable.
* Struggled with background colors and light-blue player rows.
* Required the user to manually sort uploads and specify the screenshot type.

### After (Cloud Vision API) ✅
* Zero local dependencies; lightweight pure-Go Docker image.
* GCP natively ignores background colors and UI noise.
* Automatic tab-color detection routes VS Points vs. Power Rankings flawlessly.
* Stitching drastically reduces API calls, easily supporting 100+ image uploads in seconds.

## Technical Requirements

* **Google Cloud Console:** Cloud Vision API enabled.
* **Credentials:** A Service Account JSON key must be securely uploaded via the Alliance Manager's Admin Security tab.
* **Memory:** No special requirements. The system dynamically streams and chunks images to prevent memory spikes.

## Logging & Debugging

The system provides detailed tracing in the server stdout:

```text
[INFO] Image format for tab detection: png, bounds: (0,0)-(1080,2404)
[INFO] Day detected by color: monday (confidence score: 142)
[INFO] Processing bucket: monday with 28 images
[INFO] Stitching sub-chunk 1 for bucket monday (10 images, ~11450px tall)
[INFO] GCP Extracted Text (Bucket: monday): ...
[INFO] Parsed VS Points (State Machine): dvdAlbert91 -> 50914631
```