package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	gosseract "github.com/otiai10/gosseract/v2"
)

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

		tabCenter := startX + tabWidth/2
		sampleWidth := int(float64(tabWidth) * 0.70)
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

				if r8 > 200 && g8 > 200 && b8 > 200 {
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

	minThreshold := 100
	if selectedDay >= 0 && maxLight > minThreshold {
		log.Printf("Day detected by color: %s", days[selectedDay])
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

	tabTop := int(float64(height) * 0.08)
	tabBottom := int(float64(height) * 0.105)

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

	dayByColor := detectDayByColor(tabRegion)
	if dayByColor != "" {
		return dayByColor
	}

	// Fallback to OCR
	scaledTab := scaleImage(tabRegion, 2)
	grayTab := convertToGrayscale(scaledTab)

	var buf bytes.Buffer
	if err := png.Encode(&buf, grayTab); err != nil {
		return ""
	}

	client := gosseract.NewClient()
	defer client.Close()

	if err := client.SetImageFromBytes(buf.Bytes()); err != nil {
		return ""
	}

	client.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
	text, err := client.Text()
	if err != nil || len(strings.TrimSpace(text)) == 0 {
		return ""
	}

	textLower := strings.ToLower(text)
	days := []struct {
		name     string
		patterns []string
	}{
		{"monday", []string{"monday", "mon.", "mon"}},
		{"tuesday", []string{"tuesday", "tues.", "tues", "tue"}},
		{"wednesday", []string{"wednesday", "wed.", "wed"}},
		{"thursday", []string{"thursday", "thur.", "thur", "thu"}},
		{"friday", []string{"friday", "fri.", "fri"}},
		{"saturday", []string{"saturday", "sat.", "sat"}},
	}

	for _, day := range days {
		for _, pattern := range day.patterns {
			if strings.Contains(textLower, pattern) {
				idx := strings.Index(textLower, pattern)
				if idx < 100 {
					return day.name
				}
			}
		}
	}

	return ""
}

// Extract VS points data from image and detect which day
func extractVSPointsDataFromImage(imageData []byte) (day string, records []struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error error) {
	detectedDay := detectDayFromTabRegion(imageData)

	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode image: %v", err)
	}

	attrs := analyzeScreenshot(img)

	records, err = extractVSPointsByRows(img, attrs)

	if err != nil || len(records) == 0 {
		records, err = extractVSPointsFullImage(imageData, attrs)
		if err != nil {
			return detectedDay, nil, err
		}
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

	if len(records) == 0 {
		return "", nil, fmt.Errorf("no valid VS point records found in extracted text")
	}

	return detectedDay, records, nil
}

// Extract VS points by segmenting image into rows
func extractVSPointsByRows(img image.Image, attrs *ScreenshotAttributes) ([]struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error) {
	bounds := img.Bounds()
	dataRegion := attrs.DataRegion
	rowHeight := attrs.RowHeight
	estimatedRows := attrs.EstimatedRows

	if estimatedRows < 1 {
		estimatedRows = 10
	}

	records := []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}{}

	for i := 0; i < estimatedRows; i++ {
		rowTop := dataRegion.Top + (i * rowHeight)
		rowBottom := rowTop + rowHeight

		if rowBottom > dataRegion.Bottom {
			rowBottom = dataRegion.Bottom
		}
		if rowTop >= rowBottom {
			break
		}

		rowImg := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), rowBottom-rowTop))
		draw.Draw(rowImg, rowImg.Bounds(), img, image.Point{0, rowTop}, draw.Src)

		rankWidth := bounds.Dx() * 15 / 100
		nameStart := rankWidth
		nameWidth := bounds.Dx() * 50 / 100
		pointsStart := nameStart + nameWidth

		nameImg := image.NewRGBA(image.Rect(0, 0, nameWidth, rowBottom-rowTop))
		draw.Draw(nameImg, nameImg.Bounds(), rowImg, image.Point{nameStart, 0}, draw.Src)

		pointsWidth := bounds.Dx() - pointsStart
		pointsImg := image.NewRGBA(image.Rect(0, 0, pointsWidth, rowBottom-rowTop))
		draw.Draw(pointsImg, pointsImg.Bounds(), rowImg, image.Point{pointsStart, 0}, draw.Src)

		scaledName := scaleImage(nameImg, 2)
		grayName := convertToGrayscale(scaledName)

		scaledPoints := scaleImage(pointsImg, 2)
		grayPoints := convertToGrayscale(scaledPoints)

		var nameBuf bytes.Buffer
		if err := png.Encode(&nameBuf, grayName); err != nil {
			continue
		}

		nameClient := gosseract.NewClient()
		defer nameClient.Close()
		nameClient.SetImageFromBytes(nameBuf.Bytes())
		nameClient.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
		nameText, err := nameClient.Text()
		if err != nil || len(strings.TrimSpace(nameText)) == 0 {
			continue
		}

		var pointsBuf bytes.Buffer
		if err := png.Encode(&pointsBuf, grayPoints); err != nil {
			continue
		}

		pointsClient := gosseract.NewClient()
		defer pointsClient.Close()
		pointsClient.SetImageFromBytes(pointsBuf.Bytes())
		pointsClient.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
		pointsText, err := pointsClient.Text()
		if err != nil || len(strings.TrimSpace(pointsText)) == 0 {
			continue
		}

		name := strings.TrimSpace(nameText)
		name = cleanPlayerName(name)

		pointsStr := strings.TrimSpace(pointsText)
		pointsStr = strings.ReplaceAll(pointsStr, ",", "")
		pointsStr = strings.ReplaceAll(pointsStr, ".", "")
		pointsStr = strings.ReplaceAll(pointsStr, " ", "")

		points, err := strconv.ParseInt(pointsStr, 10, 64)
		if err != nil || points < 100 {
			continue
		}

		records = append(records, struct {
			MemberName string `json:"member_name"`
			Points     int64  `json:"points"`
		}{
			MemberName: name,
			Points:     points,
		})
	}

	return records, nil
}

// Fallback: Extract VS points from full image
func extractVSPointsFullImage(imageData []byte, attrs *ScreenshotAttributes) ([]struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error) {
	processedData, err := preprocessImageForOCR(imageData)
	if err != nil {
		processedData = imageData
	}

	client := gosseract.NewClient()
	defer client.Close()

	err = client.SetImageFromBytes(processedData)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %v", err)
	}

	var text string
	psmModes := []gosseract.PageSegMode{
		gosseract.PSM_AUTO,
		gosseract.PSM_SINGLE_BLOCK,
		gosseract.PSM_SPARSE_TEXT,
	}

	for _, mode := range psmModes {
		client.SetPageSegMode(mode)
		extractedText, err := client.Text()
		if err == nil && len(strings.TrimSpace(extractedText)) > 0 {
			text = extractedText
			break
		}
	}

	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("OCR failed")
	}

	records := parseVSPointsText(text)
	return records, nil
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

// Parse VS points text(from OCR or manual input)
func parseVSPointsText(text string) []struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
} {
	var records []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}

	lines := strings.Split(text, "\n")

	rankPattern := regexp.MustCompile(`(?:R[0-9]\s+)?([A-Za-z][A-Za-z0-9_\s]*?)\s+([0-9]{6,})`)
	simplePattern := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]+)\s+([0-9]{6,})`)

	seenNames := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 5 {
			continue
		}

		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "ranking") ||
			strings.Contains(lowerLine, "commander") ||
			strings.Contains(lowerLine, "points") ||
			strings.Contains(lowerLine, "daily") ||
			strings.Contains(lowerLine, "weekly") ||
			strings.Contains(lowerLine, "mon") ||
			strings.Contains(lowerLine, "tues") ||
			strings.Contains(lowerLine, "wed") ||
			strings.Contains(lowerLine, "thur") ||
			strings.Contains(lowerLine, "fri") ||
			strings.Contains(lowerLine, "sat") ||
			strings.Contains(lowerLine, "alliance") ||
			strings.Contains(lowerLine, "your alliance") {
			continue
		}

		matches := rankPattern.FindStringSubmatch(line)
		if len(matches) == 0 {
			matches = simplePattern.FindStringSubmatch(line)
		}

		if len(matches) >= 3 {
			name := strings.TrimSpace(matches[1])
			pointsStr := strings.ReplaceAll(matches[2], ",", "")
			pointsStr = strings.ReplaceAll(pointsStr, " ", "")

			points, err := strconv.ParseInt(pointsStr, 10, 64)

			if err == nil && points >= 10000 && points <= 999999999 &&
				len(name) >= 3 && len(name) <= 30 && !seenNames[name] {
				records = append(records, struct {
					MemberName string `json:"member_name"`
					Points     int64  `json:"points"`
				}{
					MemberName: name,
					Points:     points,
				})
				seenNames[name] = true
			}
		}
	}

	return records
}

// Analyze screenshot to detect distinct regions and attributes
func analyzeScreenshot(img image.Image) *ScreenshotAttributes {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	attrs := &ScreenshotAttributes{
		Width:  width,
		Height: height,
	}

	// Detect dark title bar at top (typically 5-10% of height)
	// Title bars are usually dark colored
	titleBarHeight := height / 15 // ~6-7%
	if titleBarHeight < 30 {
		titleBarHeight = 30
	}
	attrs.TitleBarRegion = &ImageRegion{
		Name:   "TitleBar",
		Top:    0,
		Bottom: titleBarHeight,
		Left:   0,
		Right:  width,
	}

	// Detect tabs region (typically right below title bar, ~5-8% of height)
	tabsHeight := height / 15
	if tabsHeight < 40 {
		tabsHeight = 40
	}
	attrs.TabsRegion = &ImageRegion{
		Name:   "Tabs",
		Top:    titleBarHeight,
		Bottom: titleBarHeight + tabsHeight,
		Left:   0,
		Right:  width,
	}

	// Detect column headers (below tabs, ~5% of height)
	headerHeight := height / 20
	if headerHeight < 30 {
		headerHeight = 30
	}
	haederTop := titleBarHeight + tabsHeight
	attrs.HeaderRegion = &ImageRegion{
		Name:   "Headers",
		Top:    haederTop,
		Bottom: haederTop + headerHeight,
		Left:   0,
		Right:  width,
	}

	// Detect bottom button region (typically last 8-10% of height)
	buttonHeight := height / 10
	if buttonHeight < 50 {
		buttonHeight = 50
	}
	attrs.ButtonRegion = &ImageRegion{
		Name:   "BottomButton",
		Top:    height - buttonHeight,
		Bottom: height,
		Left:   0,
		Right:  width,
	}

	// Data region is everything between headers and bottom button
	dataTop := haederTop + headerHeight
	dataBottom := height - buttonHeight
	attrs.DataRegion = &ImageRegion{
		Name:   "DataRows",
		Top:    dataTop,
		Bottom: dataBottom,
		Left:   0,
		Right:  width,
	}

	// Estimate row height and count
	dataHeight := dataBottom - dataTop
	attrs.RowHeight = dataHeight / 10 // Assume ~10 visible rows
	if attrs.RowHeight < 40 {
		attrs.RowHeight = 40
	}
	attrs.EstimatedRows = dataHeight / attrs.RowHeight

	log.Printf("Screenshot Analysis: %dx%d, DataRegion: (%d,%d) to (%d,%d), Est. Rows: %d",
		width, height, attrs.DataRegion.Left, attrs.DataRegion.Top,
		attrs.DataRegion.Right, attrs.DataRegion.Bottom, attrs.EstimatedRows)

	return attrs
}

// Crop image to data region only
func cropToDataRegion(img image.Image, region *ImageRegion) image.Image {
	bounds := img.Bounds()
	top := region.Top
	bottom := region.Bottom
	left := region.Left
	right := region.Right

	// Ensure bounds are valid
	if top < bounds.Min.Y {
		top = bounds.Min.Y
	}
	if bottom > bounds.Max.Y {
		bottom = bounds.Max.Y
	}
	if left < bounds.Min.X {
		left = bounds.Min.X
	}
	if right > bounds.Max.X {
		right = bounds.Max.X
	}

	croppedImg := image.NewRGBA(image.Rect(0, 0, right-left, bottom-top))
	draw.Draw(croppedImg, croppedImg.Bounds(), img, image.Point{left, top}, draw.Src)

	log.Printf("Cropped image from %v to %v", bounds, croppedImg.Bounds())
	return croppedImg
}

// Convert image to grayscale
func convertToGrayscale(img image.Image) *image.Gray {
	bounds := img.Bounds()
	gray := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.Set(x, y, img.At(x, y))
		}
	}

	return gray
}

// Enhance contrast using histogram equalization (simplified)
func enhanceContrast(img *image.Gray) *image.Gray {
	bounds := img.Bounds()
	enhanced := image.NewGray(bounds)

	// Calculate histogram
	histogram := make([]int, 256)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayVal := img.GrayAt(x, y).Y
			histogram[grayVal]++
		}
	}

	// Calculate cumulative distribution
	totalPixels := bounds.Dx() * bounds.Dy()
	cdf := make([]float64, 256)
	cdf[0] = float64(histogram[0]) / float64(totalPixels)
	for i := 1; i < 256; i++ {
		cdf[i] = cdf[i-1] + float64(histogram[i])/float64(totalPixels)
	}

	// Apply equalization
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayVal := img.GrayAt(x, y).Y
			newVal := uint8(cdf[grayVal] * 255)
			enhanced.SetGray(x, y, color.Gray{Y: newVal})
		}
	}

	return enhanced
}

// Apply adaptive thresholding to enhance text
func applyAdaptiveThreshold(img *image.Gray, blockSize int) *image.Gray {
	bounds := img.Bounds()
	thresholded := image.NewGray(bounds)

	halfBlock := blockSize / 2

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Calculate local mean in block
			sum := 0
			count := 0
			for by := y - halfBlock; by <= y+halfBlock; by++ {
				for bx := x - halfBlock; bx <= x+halfBlock; bx++ {
					if bx >= bounds.Min.X && bx < bounds.Max.X && by >= bounds.Min.Y && by < bounds.Max.Y {
						sum += int(img.GrayAt(bx, by).Y)
						count++
					}
				}
			}
			mean := uint8(sum / count)

			// Threshold: if pixel is darker than local mean, make it black, else white
			pixel := img.GrayAt(x, y).Y
			if pixel < mean-10 { // -10 for bias towards text
				thresholded.SetGray(x, y, color.Gray{Y: 0}) // Black (text)
			} else {
				thresholded.SetGray(x, y, color.Gray{Y: 255}) // White (background)
			}
		}
	}

	return thresholded
}

// Invert image (make text black on white background)
func invertImage(img *image.Gray) *image.Gray {
	bounds := img.Bounds()
	inverted := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayVal := img.GrayAt(x, y).Y
			inverted.SetGray(x, y, color.Gray{Y: 255 - grayVal})
		}
	}

	return inverted
}

// Scale image up for better OCR
func scaleImage(img image.Image, factor int) image.Image {
	bounds := img.Bounds()
	newWidth := bounds.Dx() * factor
	newHeight := bounds.Dy() * factor

	scaled := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Simple nearest-neighbor scaling
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			origX := x / factor
			origY := y / factor
			scaled.Set(x, y, img.At(origX, origY))
		}
	}

	return scaled
}

// Preprocess image for better OCR
func preprocessImageForOCR(imageData []byte) ([]byte, error) {
	// Decode image
	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %v", err)
	}

	log.Printf("Original image: %dx%d, format: %s", img.Bounds().Dx(), img.Bounds().Dy(), format)

	// Analyze screenshot to detect regions
	attrs := analyzeScreenshot(img)

	// Crop to data region only (remove UI elements)
	// For narrow screenshots or when power values might be cut off, use less aggressive cropping
	croppedImg := img
	if attrs.DataRegion != nil && attrs.Width > 600 {
		croppedImg = cropToDataRegion(img, attrs.DataRegion)
	} else {
		log.Printf("Skipping crop for narrow image to preserve power values")
	}

	// Scale up 2x for better OCR (small text is hard to read)
	scaledImg := scaleImage(croppedImg, 2)

	// Convert to grayscale
	grayImg := convertToGrayscale(scaledImg)

	// For small images, skip contrast enhancement (can make things worse)
	processedImg := grayImg

	// Encode back to bytes
	var buf bytes.Buffer
	if err := png.Encode(&buf, processedImg); err != nil {
		return nil, fmt.Errorf("failed to encode processed image: %v", err)
	}

	log.Printf("Image preprocessed: %dx%d -> %dx%d (2x scaled grayscale)",
		img.Bounds().Dx(), img.Bounds().Dy(),
		processedImg.Bounds().Dx(), processedImg.Bounds().Dy())

	return buf.Bytes(), nil
}

// Extract power data from image using OCR with preprocessing
func extractPowerDataFromImage(imageData []byte) ([]struct {
	MemberName string `json:"member_name"`
	Power      int64  `json:"power"`
}, error) {
	// Preprocess image to filter and enhance relevant regions
	processedData, err := preprocessImageForOCR(imageData)
	if err != nil {
		log.Printf("Warning: Image preprocessing failed: %v. Using original image.", err)
		processedData = imageData // Fallback to original
	}

	client := gosseract.NewClient()
	defer client.Close()

	err = client.SetImageFromBytes(processedData)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %v", err)
	}

	// Try different PSM modes for better recognition
	var text string
	psmModes := []gosseract.PageSegMode{
		gosseract.PSM_AUTO,
		gosseract.PSM_SINGLE_BLOCK,
		gosseract.PSM_SPARSE_TEXT,
	}

	for i, mode := range psmModes {
		client.SetPageSegMode(mode)
		extractedText, err := client.Text()
		if err == nil && len(strings.TrimSpace(extractedText)) > 0 {
			text = extractedText
			log.Printf("OCR successful with PSM mode %d (attempt %d)", mode, i+1)
			break
		}
		log.Printf("OCR attempt %d with PSM mode %d failed or empty", i+1, mode)
	}

	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("OCR failed: no text extracted after trying multiple modes")
	}

	// Log the extracted text for debugging
	log.Printf("OCR extracted text:\n%s\n---END OCR---", text)

	// Parse the OCR text
	records := parsePowerRankingsText(text)

	if len(records) == 0 {
		return nil, fmt.Errorf("no valid records found in extracted text (see server logs for OCR output)")
	}

	return records, nil
}

// Parse power rankings text (from OCR or manual input)
func parsePowerRankingsText(text string) []struct {
	MemberName string `json:"member_name"`
	Power      int64  `json:"power"`
} {
	var records []struct {
		MemberName string `json:"member_name"`
		Power      int64  `json:"power"`
	}

	lines := strings.Split(text, "\n")

	// Pattern specifically for Last War rankings format
	// Matches: optional rank badge (R4, R3), name (can have spaces), then large power number
	// Examples: "R4 Gary6126 77421000", "Nutty Tx 61926102", "R3 DYNOSUR 63785308"
	// Updated to better handle multi-word names
	rankPattern := regexp.MustCompile(`(?:R[0-9]\s+)?([A-Za-z][A-Za-z0-9_\s]+?)\s+([0-9]{7,})`)

	// Alternative simpler pattern: captures name with spaces followed by 7+ digit number
	simplePattern := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_\s]+?)\s+([0-9]{7,})`)

	// Pattern for lines with rank number prefix: "1 Gary6126 R4 77421000" or "1 ileesu R4 66715876"
	rankPrefixPattern := regexp.MustCompile(`^[0-9]{1,3}\s+([A-Za-z][A-Za-z0-9_\s]+?)\s+(?:R[0-9]\s+)?([0-9]{7,})`)

	// Flexible pattern that allows letters in power (for OCR errors): "B 25) Nutty Tx s1926102"
	// This captures name followed by 7+ chars that may contain letters misread as digits
	flexiblePattern := regexp.MustCompile(`(?:[A-Z]{1,3}\s+)?(?:\d+\)?\s+)?([A-Za-z][A-Za-z0-9_\s]+?)\s+([A-Za-z0-9]{7,})`)

	// Track seen names to avoid duplicates from multi-line OCR
	seenNames := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip lines that are clearly UI elements or rank numbers
		if len(line) < 5 || regexp.MustCompile(`^[0-9]{1,2}$`).MatchString(line) {
			continue
		}

		// Skip common UI text
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "ranking") ||
			strings.Contains(lowerLine, "commander") ||
			strings.Contains(lowerLine, "power") ||
			strings.Contains(lowerLine, "kills") ||
			strings.Contains(lowerLine, "donation") {
			continue
		}

		// Try rank pattern first (for lines with R4, R3, etc.)
		matches := rankPattern.FindStringSubmatch(line)
		if len(matches) == 0 {
			// Try pattern with rank number prefix
			matches = rankPrefixPattern.FindStringSubmatch(line)
		}
		if len(matches) == 0 {
			// Try simple pattern
			matches = simplePattern.FindStringSubmatch(line)
		}
		if len(matches) == 0 {
			// Try flexible pattern (allows letters in power number for OCR errors)
			matches = flexiblePattern.FindStringSubmatch(line)
		}

		if len(matches) >= 3 {
			name := strings.TrimSpace(matches[1])
			// Clean up extra whitespace in names
			name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")

			powerStr := strings.ReplaceAll(matches[2], ",", "")
			powerStr = strings.ReplaceAll(powerStr, " ", "")
			powerStr = strings.ReplaceAll(powerStr, ".", "") // Remove periods OCR might insert
			// Common OCR character misreads for digits
			powerStr = strings.ReplaceAll(powerStr, "O", "0")
			powerStr = strings.ReplaceAll(powerStr, "o", "0")
			powerStr = strings.ReplaceAll(powerStr, "s", "6") // s often misread as 6
			powerStr = strings.ReplaceAll(powerStr, "S", "5") // S often misread as 5
			powerStr = strings.ReplaceAll(powerStr, "l", "1") // l often misread as 1
			powerStr = strings.ReplaceAll(powerStr, "I", "1") // I often misread as 1
			powerStr = strings.ReplaceAll(powerStr, "Z", "2") // Z sometimes misread as 2
			powerStr = strings.ReplaceAll(powerStr, "B", "8") // B sometimes misread as 8
			powerStr = strings.ReplaceAll(powerStr, "e", "6") // e sometimes misread as 6
			powerStr = strings.ReplaceAll(powerStr, "g", "9") // g sometimes misread as 9
			powerStr = strings.ReplaceAll(powerStr, "G", "6") // G sometimes misread as 6
			// Remove any remaining non-digit characters
			powerStr = regexp.MustCompile(`[^0-9]`).ReplaceAllString(powerStr, "")

			power, err := strconv.ParseInt(powerStr, 10, 64)

			// Validate: power should be realistic (1M to 1B range), name should be reasonable
			if err == nil && power >= 1000000 && power <= 9999999999 &&
				len(name) >= 3 && len(name) <= 30 && !seenNames[name] {
				records = append(records, struct {
					MemberName string `json:"member_name"`
					Power      int64  `json:"power"`
				}{
					MemberName: name,
					Power:      power,
				})
				seenNames[name] = true
				log.Printf("Parsed: %s -> %d", name, power)
			} else if err != nil {
				log.Printf("Failed to parse power for %s: %s (error: %v)", name, powerStr, err)
			}
		}
	}

	return records
}
