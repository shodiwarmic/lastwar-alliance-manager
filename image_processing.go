// image_processing.go - Contains functions for processing uploaded screenshots, extracting data using Google Cloud Vision OCR, and detecting which day tab is selected based on color and text analysis.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	vision "cloud.google.com/go/vision/apiv1"
	"google.golang.org/api/option"
)

// getGCPClient securely fetches, decrypts, and initializes the Google Cloud Vision client.
func getGCPClient(ctx context.Context) (*vision.ImageAnnotatorClient, error) {
	var encryptedBlob, nonce []byte

	err := db.QueryRow("SELECT encrypted_blob, nonce FROM credentials WHERE service_name = 'gcp_vision'").Scan(&encryptedBlob, &nonce)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("GCP Vision credentials not configured by admin")
		}
		return nil, fmt.Errorf("database error retrieving credentials: %v", err)
	}

	hexKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if hexKey == "" {
		return nil, fmt.Errorf("server encryption key missing")
	}

	plaintextJSON, err := Decrypt(encryptedBlob, nonce, hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt GCP credentials: %v", err)
	}

	defer func() {
		for i := range plaintextJSON {
			plaintextJSON[i] = 0
		}
	}()

	client, err := vision.NewImageAnnotatorClient(ctx, option.WithCredentialsJSON(plaintextJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP client: %v", err)
	}

	return client, nil
}

// stitchImagesVertically combines multiple screenshots into a single tall image to save API quotas
func stitchImagesVertically(imageDatas [][]byte) ([]byte, error) {
	if len(imageDatas) == 0 {
		return nil, fmt.Errorf("no images provided for stitching")
	}

	if len(imageDatas) == 1 {
		return imageDatas[0], nil
	}

	var decodedImages []image.Image
	var totalHeight, maxWidth int

	for _, data := range imageDatas {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode an image for stitching: %v", err)
		}
		decodedImages = append(decodedImages, img)

		bounds := img.Bounds()
		totalHeight += bounds.Dy()
		if bounds.Dx() > maxWidth {
			maxWidth = bounds.Dx()
		}
	}

	stitchedCanvas := image.NewRGBA(image.Rect(0, 0, maxWidth, totalHeight))
	currentY := 0

	for _, img := range decodedImages {
		bounds := img.Bounds()
		draw.Draw(stitchedCanvas, image.Rect(0, currentY, bounds.Dx(), currentY+bounds.Dy()), img, bounds.Min, draw.Src)
		currentY += bounds.Dy()
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, stitchedCanvas); err != nil {
		return nil, fmt.Errorf("failed to encode stitched image: %v", err)
	}

	log.Printf("Successfully stitched %d images. New dimensions: %dx%d", len(imageDatas), maxWidth, totalHeight)
	return buf.Bytes(), nil
}

// Detect selected day tab by color
func detectDayByColor(img image.Image) string {
	bounds := img.Bounds()
	width := bounds.Dx()

	days := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	tabWidth := width / 6

	lightCounts := make([]int, 6)

	for dayIdx := 0; dayIdx < 6; dayIdx++ {
		startX := dayIdx * tabWidth
		endX := startX + tabWidth
		if dayIdx == 5 {
			endX = width
		}

		// Sample the middle 50% of the column to avoid borders and edge anti-aliasing
		tabCenter := startX + tabWidth/2
		sampleWidth := int(float64(tabWidth) * 0.50)
		sampleStartX := tabCenter - sampleWidth/2
		sampleEndX := tabCenter + sampleWidth/2

		if sampleStartX < startX {
			sampleStartX = startX
		}
		if sampleEndX > endX {
			sampleEndX = endX
		}

		lightCount := 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := sampleStartX; x < sampleEndX; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)

				// STRICT threshold: The active tab is pure white (#FFFFFF).
				// This ignores light gray headers (>200) and targets the actual tab.
				if r8 > 240 && g8 > 240 && b8 > 240 {
					lightCount++
				}
			}
		}
		lightCounts[dayIdx] = lightCount
	}

	maxLight := 0
	selectedDay := -1
	for i, count := range lightCounts {
		if count > maxLight {
			maxLight = count
			selectedDay = i
		}
	}

	// Require a solid block of white pixels to prevent false positives from stray text
	minThreshold := 50
	if selectedDay >= 0 && maxLight > minThreshold {
		log.Printf("Day detected by color: %s (confidence score: %d)", days[selectedDay], maxLight)
		return days[selectedDay]
	}

	return ""
}

// Extract just the day tab region and detect selected day by color
func detectDayFromTabRegion(imageData []byte) string {
	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return ""
	}
	log.Printf("Image format for tab detection: %s, bounds: %v", format, img.Bounds())

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Expanded and lowered scan area (11% to 22%) to bypass the "RANKING" title
	// and accurately hit the day tabs across varying screen aspect ratios.
	tabTop := int(float64(height) * 0.11)
	tabBottom := int(float64(height) * 0.22)

	if tabTop < 0 {
		tabTop = 0
	}
	if tabBottom > height {
		tabBottom = height
	}
	if tabBottom <= tabTop {
		tabBottom = tabTop + 100
	}

	tabRegion := image.NewRGBA(image.Rect(0, 0, width, tabBottom-tabTop))
	draw.Draw(tabRegion, tabRegion.Bounds(), img, image.Point{0, tabTop}, draw.Src)

	return detectDayByColor(tabRegion)
}

// Extract VS points data from an array of images
func extractVSPointsDataFromImages(imageDatas [][]byte) (day string, records []struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error error) {
	if len(imageDatas) == 0 {
		return "", nil, fmt.Errorf("no images provided")
	}

	detectedDay := detectDayFromTabRegion(imageDatas[0])

	stitchedBytes, err := stitchImagesVertically(imageDatas)
	if err != nil {
		return detectedDay, nil, fmt.Errorf("stitching failed: %v", err)
	}

	ctx := context.Background()
	client, err := getGCPClient(ctx)
	if err != nil {
		return detectedDay, nil, err
	}
	defer client.Close()

	img, err := vision.NewImageFromReader(bytes.NewReader(stitchedBytes))
	if err != nil {
		return detectedDay, nil, fmt.Errorf("failed to prepare image for GCP: %v", err)
	}

	annotation, err := client.DetectDocumentText(ctx, img, nil)
	if err != nil {
		return detectedDay, nil, fmt.Errorf("GCP Vision API error: %v", err)
	}

	if annotation == nil || annotation.Text == "" {
		return detectedDay, nil, fmt.Errorf("GCP returned no text")
	}

	log.Printf("GCP Extracted Text (VS Points Stitched):\n%s", annotation.Text)

	if detectedDay == "" {
		detectedDay = detectSelectedDay(annotation.Text)
	}

	if detectedDay == "" {
		now := time.Now()
		weekday := now.Weekday()
		switch weekday {
		case time.Monday:
			detectedDay = "monday"
		case time.Tuesday:
			detectedDay = "tuesday"
		case time.Wednesday:
			detectedDay = "wednesday"
		case time.Thursday:
			detectedDay = "thursday"
		case time.Friday:
			detectedDay = "friday"
		case time.Saturday:
			detectedDay = "saturday"
		default:
			detectedDay = "monday"
		}
	}

	records = parseVSPointsText(annotation.Text)
	if len(records) == 0 {
		return "", nil, fmt.Errorf("no valid VS point records found in extracted text")
	}

	return detectedDay, records, nil
}

// Extract power data from an array of images using GCP Vision OCR
func extractPowerDataFromImages(imageDatas [][]byte) ([]struct {
	MemberName string `json:"member_name"`
	Power      int64  `json:"power"`
}, error) {
	if len(imageDatas) == 0 {
		return nil, fmt.Errorf("no images provided")
	}

	stitchedBytes, err := stitchImagesVertically(imageDatas)
	if err != nil {
		return nil, fmt.Errorf("stitching failed: %v", err)
	}

	ctx := context.Background()
	client, err := getGCPClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	img, err := vision.NewImageFromReader(bytes.NewReader(stitchedBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare image for GCP: %v", err)
	}

	annotation, err := client.DetectDocumentText(ctx, img, nil)
	if err != nil {
		return nil, fmt.Errorf("GCP Vision API error: %v", err)
	}

	if annotation == nil || annotation.Text == "" {
		return nil, fmt.Errorf("GCP returned no text")
	}

	log.Printf("GCP Extracted Text (Power Stitched):\n%s", annotation.Text)

	records := parsePowerRankingsText(annotation.Text)
	if len(records) == 0 {
		return nil, fmt.Errorf("no valid records found in extracted text")
	}

	return records, nil
}

// isStrictlyNumeric checks if a string contains ONLY digits
func isStrictlyNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Clean player name by removing alliance tags, special characters, etc
func cleanPlayerName(name string) string {
	name = strings.ReplaceAll(name, "|", "I")
	name = strings.ReplaceAll(name, "~", "")
	name = strings.ReplaceAll(name, "`", "")

	re := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	name = re.ReplaceAllString(name, "")

	re = regexp.MustCompile(`^\d+\)?\s*`)
	name = re.ReplaceAllString(name, "")

	return strings.TrimSpace(name)
}

// Detect which day is selected from OCR text
func detectSelectedDay(text string) string {
	textLower := strings.ToLower(text)

	if !strings.Contains(textLower, "daily") && !strings.Contains(textLower, "rank") {
		return ""
	}

	days := map[string][]string{
		"monday":    {"monday", "mon.", "mon"},
		"tuesday":   {"tuesday", "tues.", "tues"},
		"wednesday": {"wednesday", "wed.", "wed"},
		"thursday":  {"thursday", "thur.", "thur"},
		"friday":    {"friday", "fri.", "fri"},
		"saturday":  {"saturday", "sat.", "sat"},
	}

	dayScores := make(map[string]int)

	for standardDay, variants := range days {
		for _, variant := range variants {
			count := strings.Count(textLower, variant)
			dayScores[standardDay] += count

			if idx := strings.Index(textLower, variant); idx >= 0 && idx < 200 {
				dayScores[standardDay] += 2
			}
		}
	}

	maxScore := 0
	selectedDay := ""
	for day, score := range dayScores {
		if score > maxScore {
			maxScore = score
			selectedDay = day
		}
	}

	if maxScore >= 3 {
		return selectedDay
	}

	return ""
}

// Parse VS points text using a Hybrid Regex + State Machine logic for GCP vertical lists
func parseVSPointsText(text string) []struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
} {
	var records []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}

	lines := strings.Split(text, "\n")
	seenNames := make(map[string]bool)
	lastNameSeen := ""

	// Hybrid Regex: In case GCP reads them on the same line horizontally
	singleLineRegex := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_\s]+)\s+([0-9]{5,})`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// 1. Check if the line contains BOTH a name and a score horizontally
		cleanLine := strings.ReplaceAll(line, ",", "")
		cleanLine = strings.ReplaceAll(cleanLine, ".", "")

		matches := singleLineRegex.FindStringSubmatch(cleanLine)
		if len(matches) >= 3 {
			name := cleanPlayerName(matches[1])
			points, _ := strconv.ParseInt(matches[2], 10, 64)
			if points >= 10000 && len(name) >= 3 && !seenNames[name] {
				records = append(records, struct {
					MemberName string `json:"member_name"`
					Points     int64  `json:"points"`
				}{
					MemberName: name,
					Points:     points,
				})
				seenNames[name] = true
				lastNameSeen = "" // reset
			}
			continue
		}

		// 2. Filter out explicit junk lines
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "ranking") ||
			strings.Contains(lowerLine, "commander") ||
			strings.Contains(lowerLine, "points") ||
			strings.Contains(lowerLine, "daily rank") ||
			strings.Contains(lowerLine, "weekly rank") ||
			strings.Contains(lowerLine, "mon.") ||
			strings.Contains(lowerLine, "tues.") ||
			strings.Contains(lowerLine, "wed.") ||
			strings.Contains(lowerLine, "thur.") ||
			strings.Contains(lowerLine, "fri.") ||
			strings.Contains(lowerLine, "sat.") ||
			strings.Contains(lowerLine, "alliance") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			continue // Standalone Alliance Tag
		}

		// 3. State Machine check: Is this line a number?
		numTest := cleanLine
		numTest = strings.ReplaceAll(numTest, " ", "")
		numTest = strings.ReplaceAll(numTest, "O", "0")
		numTest = strings.ReplaceAll(numTest, "o", "0")

		if isStrictlyNumeric(numTest) {
			val, err := strconv.ParseInt(numTest, 10, 64)
			if err == nil {
				if val >= 10000 { // This is a Score!
					if lastNameSeen != "" {
						cleanName := cleanPlayerName(lastNameSeen)
						if len(cleanName) >= 3 && !seenNames[cleanName] {
							records = append(records, struct {
								MemberName string `json:"member_name"`
								Points     int64  `json:"points"`
							}{
								MemberName: cleanName,
								Points:     val,
							})
							seenNames[cleanName] = true
							log.Printf("Parsed VS Points (State Machine): %s -> %d", cleanName, val)
						}
						lastNameSeen = "" // reset
					}
				}
				// If val < 10000, it's a Rank (e.g. "1", "2"). Ignore it, keeping lastNameSeen intact.
				continue
			}
		}

		// 4. If it's short, it's probably a misread Rank Badge (e.g. 'B')
		if len(line) <= 2 || regexp.MustCompile(`(?i)^R[1-5]$`).MatchString(line) {
			continue
		}

		// 5. If it survived all that, it is the Player Name!
		lastNameSeen = line
	}

	return records
}

// Parse power rankings text using Hybrid Regex + State Machine logic
func parsePowerRankingsText(text string) []struct {
	MemberName string `json:"member_name"`
	Power      int64  `json:"power"`
} {
	var records []struct {
		MemberName string `json:"member_name"`
		Power      int64  `json:"power"`
	}

	lines := strings.Split(text, "\n")
	seenNames := make(map[string]bool)
	lastNameSeen := ""

	singleLineRegex := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_\s]+)\s+([0-9]{6,})`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// 1. Horizontal Check
		cleanLine := strings.ReplaceAll(line, ",", "")
		cleanLine = strings.ReplaceAll(cleanLine, ".", "")

		matches := singleLineRegex.FindStringSubmatch(cleanLine)
		if len(matches) >= 3 {
			name := cleanPlayerName(matches[1])
			power, _ := strconv.ParseInt(matches[2], 10, 64)
			if power >= 1000000 && len(name) >= 3 && !seenNames[name] {
				records = append(records, struct {
					MemberName string `json:"member_name"`
					Power      int64  `json:"power"`
				}{
					MemberName: name,
					Power:      power,
				})
				seenNames[name] = true
				lastNameSeen = ""
			}
			continue
		}

		// 2. Junk Filter
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "ranking") ||
			strings.Contains(lowerLine, "commander") ||
			strings.Contains(lowerLine, "power") ||
			strings.Contains(lowerLine, "kills") ||
			strings.Contains(lowerLine, "donation") ||
			strings.Contains(lowerLine, "alliance") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			continue
		}

		// 3. State Machine Check (Is it a Power number?)
		numTest := cleanLine
		numTest = strings.ReplaceAll(numTest, " ", "")
		numTest = strings.ReplaceAll(numTest, "O", "0")
		numTest = strings.ReplaceAll(numTest, "o", "0")
		numTest = strings.ReplaceAll(numTest, "s", "6")
		numTest = strings.ReplaceAll(numTest, "S", "5")
		numTest = strings.ReplaceAll(numTest, "l", "1")
		numTest = strings.ReplaceAll(numTest, "I", "1")
		numTest = strings.ReplaceAll(numTest, "Z", "2")
		numTest = strings.ReplaceAll(numTest, "B", "8")

		// Strip remaining letters to see if it's primarily a number
		numericOnly := regexp.MustCompile(`[^0-9]`).ReplaceAllString(numTest, "")

		if len(numericOnly) >= 6 {
			power, err := strconv.ParseInt(numericOnly, 10, 64)
			if err == nil && power >= 1000000 && power <= 9999999999 {
				if lastNameSeen != "" {
					cleanName := cleanPlayerName(lastNameSeen)
					if len(cleanName) >= 3 && !seenNames[cleanName] {
						records = append(records, struct {
							MemberName string `json:"member_name"`
							Power      int64  `json:"power"`
						}{
							MemberName: cleanName,
							Power:      power,
						})
						seenNames[cleanName] = true
						log.Printf("Parsed Power (State Machine): %s -> %d", cleanName, power)
					}
					lastNameSeen = ""
				}
				continue
			}
		} else if isStrictlyNumeric(numTest) {
			// It is a valid numeric string, but < 6 digits, so it is a Rank Number (1, 2, etc.)
			continue
		}

		// 4. Short strings and Rank Badges
		if len(line) <= 2 || regexp.MustCompile(`(?i)^R[1-5]$`).MatchString(line) {
			continue
		}

		// 5. It is the Player Name!
		lastNameSeen = line
	}

	return records
}

// SmartRecord holds data that could belong to either VS Points or Power Rankings
type SmartRecord struct {
	MemberName string
	Value      int64
}

// extractSmartDataFromImages dynamically determines the screenshot type and parses the data
func extractSmartDataFromImages(imageDatas [][]byte, preDetectedDay string) (dataType string, day string, records []SmartRecord, err error) {
	if len(imageDatas) == 0 {
		return "", "", nil, fmt.Errorf("no images provided")
	}

	detectedDay := preDetectedDay
	if detectedDay == "unknown" {
		detectedDay = "" // Let text analysis figure it out
	}

	stitchedBytes, err := stitchImagesVertically(imageDatas)
	if err != nil {
		return "", "", nil, fmt.Errorf("stitching failed: %v", err)
	}

	ctx := context.Background()
	client, err := getGCPClient(ctx)
	if err != nil {
		return "", "", nil, err
	}
	defer client.Close()

	img, err := vision.NewImageFromReader(bytes.NewReader(stitchedBytes))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to prepare image for GCP: %v", err)
	}

	annotation, err := client.DetectDocumentText(ctx, img, nil)
	if err != nil {
		return "", "", nil, fmt.Errorf("GCP Vision API error: %v", err)
	}

	if annotation == nil || annotation.Text == "" {
		return "", "", nil, fmt.Errorf("GCP returned no text")
	}

	text := annotation.Text
	log.Printf("GCP Extracted Text (Bucket: %s):\n%s", preDetectedDay, text)

	// Smart Type Detection: Trust explicit text over color-tab hints
	lowerText := strings.ToLower(text)
	isVSPoints := false

	if strings.Contains(lowerText, "daily rank") || strings.Contains(lowerText, "weekly rank") || strings.Contains(lowerText, "mon.") || strings.Contains(lowerText, "tues.") {
		isVSPoints = true
	} else if strings.Contains(lowerText, "strength ranking") || strings.Contains(lowerText, "power") {
		isVSPoints = false
	} else if detectedDay != "" && detectedDay != "unknown" {
		// Fallback: If no explicit headers were found, but we saw a white tab, assume VS points
		isVSPoints = true
	}

	// Route to the correct parser
	if isVSPoints {
		dataType = "vs_points"

		// Refine the day if we know it's VS Points
		if detectedDay == "" {
			detectedDay = detectSelectedDay(text)
			if detectedDay == "" {
				now := time.Now()
				weekday := now.Weekday()
				switch weekday {
				case time.Monday:
					detectedDay = "monday"
				case time.Tuesday:
					detectedDay = "tuesday"
				case time.Wednesday:
					detectedDay = "wednesday"
				case time.Thursday:
					detectedDay = "thursday"
				case time.Friday:
					detectedDay = "friday"
				case time.Saturday:
					detectedDay = "saturday"
				default:
					detectedDay = "monday"
				}
			}
		}

		vsRecords := parseVSPointsText(text)
		for _, r := range vsRecords {
			records = append(records, SmartRecord{MemberName: r.MemberName, Value: r.Points})
		}
	} else {
		dataType = "power"
		detectedDay = "" // Day doesn't matter for power rankings

		pRecords := parsePowerRankingsText(text)
		for _, r := range pRecords {
			records = append(records, SmartRecord{MemberName: r.MemberName, Value: r.Power})
		}
	}

	if len(records) == 0 {
		return dataType, detectedDay, nil, fmt.Errorf("no valid %s records found in extracted text", dataType)
	}

	return dataType, detectedDay, records, nil
}
