/*
 * Copyright 2025 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ocr

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"strings"

	"github.com/coze-dev/coze-studio/backend/pkg/urltobase64url"
)

// prepareOpenAIVisionFileData normalizes input for OpenAI-compatible OCR endpoints.
// PDF-only models (e.g. DeepSeek-OCR wrappers) require application/pdf in message content;
// images are wrapped as a single-page PDF.
func prepareOpenAIVisionFileData(fileData *urltobase64url.FileData) (*urltobase64url.FileData, error) {
	if isPDFFileData(fileData) {
		return ensurePDFDataURL(fileData), nil
	}
	if !imageMimeTypes[fileData.MimeType] {
		return nil, fmt.Errorf("unsupported file format for OpenAI Vision: %s", fileData.MimeType)
	}
	return imageFileDataToPDF(fileData)
}

func isPDFFileData(fileData *urltobase64url.FileData) bool {
	if fileData.MimeType == "application/pdf" {
		return true
	}
	raw, err := decodeBase64DataURI(fileData.Base64Url)
	if err != nil {
		return false
	}
	return bytes.HasPrefix(bytes.TrimLeft(raw, " \t\r\n"), []byte("%PDF"))
}

func ensurePDFDataURL(fileData *urltobase64url.FileData) *urltobase64url.FileData {
	raw, err := decodeBase64DataURI(fileData.Base64Url)
	if err != nil {
		return fileData
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	return &urltobase64url.FileData{
		Base64Url: "data:application/pdf;base64," + b64,
		MimeType:  "application/pdf",
	}
}

func imageFileDataToPDF(fileData *urltobase64url.FileData) (*urltobase64url.FileData, error) {
	raw, err := decodeBase64DataURI(fileData.Base64Url)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image data: %w", err)
	}
	pdfBytes, err := imageBytesToPDF(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to convert image to PDF: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(pdfBytes)
	return &urltobase64url.FileData{
		Base64Url: "data:application/pdf;base64," + b64,
		MimeType:  "application/pdf",
	}, nil
}

func imageBytesToPDF(imageBytes []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	return buildSinglePageJPEGPDF(jpegBuf.Bytes(), bounds.Dx(), bounds.Dy())
}

// buildSinglePageJPEGPDF builds a minimal PDF 1.4 document with one DCT-compressed page.
func buildSinglePageJPEGPDF(jpeg []byte, width, height int) ([]byte, error) {
	if len(jpeg) == 0 || width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image dimensions for PDF")
	}

	wStr := strconv.Itoa(width)
	hStr := strconv.Itoa(height)
	jpegLen := strconv.Itoa(len(jpeg))

	var buf bytes.Buffer
	write := func(s string) { buf.WriteString(s) }

	write("%PDF-1.4\n")
	offsets := make([]int, 7)

	offsets[1] = buf.Len()
	write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	offsets[2] = buf.Len()
	write("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	offsets[3] = buf.Len()
	write(fmt.Sprintf("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>\nendobj\n", wStr, hStr))

	offsets[4] = buf.Len()
	write(fmt.Sprintf("4 0 obj\n<< /Type /XObject /Subtype /Image /Width %s /Height %s /ColorSpace /DeviceRGB /Filter /DCTDecode /BitsPerComponent 8 /Length %s >>\nstream\n", wStr, hStr, jpegLen))
	buf.Write(jpeg)
	write("\nendstream\nendobj\n")

	content := fmt.Sprintf("q %s 0 0 %s 0 0 cm /Im0 Do Q\n", wStr, hStr)
	contentLen := strconv.Itoa(len(content))

	offsets[5] = buf.Len()
	write(fmt.Sprintf("5 0 obj\n<< /Length %s >>\nstream\n%s\nendstream\nendobj\n", contentLen, content))

	offsets[6] = buf.Len()
	write("6 0 obj\n<< /Size 7 /Root 1 0 R >>\nendobj\n")

	xrefPos := buf.Len()
	write("xref\n0 7\n")
	write("0000000000 65535 f \n")
	for i := 1; i <= 6; i++ {
		write(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	write(fmt.Sprintf("trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefPos))

	return buf.Bytes(), nil
}

// prepareOpenAIVisionNativeFileData keeps images as image data URLs; PDF inputs stay as PDF.
// Use with message_format image_native for CPU/OpenAI-compatible OCR containers (e.g. EasyOCR).
func prepareOpenAIVisionNativeFileData(fileData *urltobase64url.FileData) (*urltobase64url.FileData, error) {
	if isPDFFileData(fileData) {
		return ensurePDFDataURL(fileData), nil
	}
	if !imageMimeTypes[fileData.MimeType] {
		return nil, fmt.Errorf("unsupported file format for OpenAI Vision image mode: %s", fileData.MimeType)
	}
	return ensureImageDataURL(fileData), nil
}

func ensureImageDataURL(fileData *urltobase64url.FileData) *urltobase64url.FileData {
	if strings.HasPrefix(fileData.Base64Url, "data:") {
		return fileData
	}
	raw, err := decodeBase64DataURI(fileData.Base64Url)
	if err != nil {
		return fileData
	}
	mime := fileData.MimeType
	if mime == "" {
		mime = "image/png"
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	return &urltobase64url.FileData{
		Base64Url: "data:" + mime + ";base64," + b64,
		MimeType:  mime,
	}
}

// buildPaddleOCRChatRequest builds the JSON body expected by PaddleOCR /v1/chat/completions wrappers.
// messages[].content is a single data:application/pdf;base64,... (or image) string; images are
// converted to PDF upstream via prepareOpenAIVisionFileData.
func buildPaddleOCRChatRequest(cfg *OpenAIVisionConfig, dataURLContent string) map[string]any {
	outputFormat := strings.TrimSpace(cfg.PaddleOCROutputFormat)
	if outputFormat == "" {
		outputFormat = "text"
	}
	return map[string]any{
		"model":  cfg.Model,
		"stream": false,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": dataURLContent,
			},
		},
		"paddleocr": map[string]any{
			"output_format": outputFormat,
		},
	}
}

// normalizeOpenAIVisionEndpoint strips accidental /v1/chat/completions suffixes from the UI.
func normalizeOpenAIVisionEndpoint(endpoint string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	for _, suffix := range []string{"/v1/chat/completions", "/chat/completions"} {
		if strings.HasSuffix(endpoint, suffix) {
			endpoint = strings.TrimSuffix(endpoint, suffix)
			endpoint = strings.TrimRight(endpoint, "/")
		}
	}
	if strings.HasSuffix(endpoint, "/v1") {
		endpoint = strings.TrimSuffix(endpoint, "/v1")
		endpoint = strings.TrimRight(endpoint, "/")
	}
	return endpoint
}

// buildOpenAIVisionImageMessageContent builds content for standard image-native
// OpenAI-compatible OCR APIs, including DeepSeek-OCR-2 vLLM wrappers.
func buildOpenAIVisionImageMessageContent(imageDataURL, prompt string) []map[string]any {
	parts := make([]map[string]any, 0, 2)
	if strings.TrimSpace(prompt) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": prompt,
		})
	}
	parts = append(parts, map[string]any{
		"type": "image_url",
		"image_url": map[string]string{
			"url": imageDataURL,
		},
	})
	return parts
}

func buildOpenAIVisionFileMessageContent(fileDataURL, prompt string) []map[string]any {
	parts := make([]map[string]any, 0, 2)
	if strings.TrimSpace(prompt) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": prompt,
		})
	}
	parts = append(parts, map[string]any{
		"type": "file",
		"file": map[string]string{
			"file_data": fileDataURL,
		},
	})
	return parts
}

// buildOpenAIVisionMessageContent builds chat message content for PDF-capable vision APIs.
// PDF-only wrappers (e.g. DeepSeek-OCR HTTP adapters) scan text parts or string content for
// data:application/pdf;base64,... — not OpenAI-style type:file parts. Include both text and file.
func buildOpenAIVisionMessageContent(pdfDataURL, prompt string) []map[string]any {
	parts := []map[string]any{
		{
			"type": "text",
			"text": pdfDataURL,
		},
		{
			"type": "file",
			"file": map[string]string{
				"file_data": pdfDataURL,
			},
		},
	}
	if strings.TrimSpace(prompt) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": prompt,
		})
	}
	return parts
}
