package agenttest

import (
	"bytes"
	"fmt"
	"strings"
)

// PDF builds a small valid PDF fixture with searchable text and colored vector
// shapes. The first page is red, later pages blue, allowing pixel-level checks.
func PDF(pages int) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	kids := make([]string, pages)
	for index := range pages {
		pageID := len(objects) + 1
		kids[index] = fmt.Sprintf("%d 0 R", pageID)
		color := "1 0 0"
		if index > 0 {
			color = "0 0 1"
		}
		content := fmt.Sprintf("%s rg 0 0 200 200 re f\n0 0 0 rg BT /F1 16 Tf 20 250 Td (VISION_739281 PAGE_%d) Tj ET", color, index+1)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> /Contents %d 0 R >>", pageID+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", pages, strings.Join(kids, " "))
	return pdfObjects(objects)
}

// ScannedPDF represents a scan: one embedded JPEG, no PDF text layer.
func ScannedPDF(jpeg []byte, width, height int) []byte {
	content := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Scan Do Q", width, height)
	return pdfObjects([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Scan 5 0 R >> >> /Contents 4 0 R >>", width, height),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", width, height, len(jpeg), jpeg),
	})
}

func pdfObjects(objects []string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for index, object := range objects {
		offsets[index] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}
