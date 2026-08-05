// Presentation extension for Mneme.
//
// Provides presentation generation tools:
//   - generate_presentation: create a .pptx file from structured content
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}
type callToolParams struct {
	Name string
	Args map[string]interface{}
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "presentation",
	Version:     "0.1.0",
	Description: "Presentation generation: create .pptx from markdown",
	Tools:       []string{"generate_presentation"},
	AgentDefs:   []string{"presentation_agent"},
	ProtocolMin: 1,
}

var agentDefs = []struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`
}{
	{
		ID: "presentation_agent", Name: "Presentation Creator",
		Description: "Creates presentation slides from structured content",
		Tier:        "worker",
		SystemPrompt: `You are a presentation creation specialist. Create well-structured slide decks from content.
- Each slide should have a clear title and bullet points
- Use the generate_presentation tool to produce .pptx files
- Keep slides concise and visually balanced`,
		ToolAllowlist: []string{"generate_presentation", "write_file", "read_file", "memory_search"},
		MaxIterations: 10, Hidden: false,
	},
}

type slide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

var toolDefs = []toolDef{
	{
		Name:        "generate_presentation",
		Description: "Generate a .pptx presentation file from structured content (JSON format with title, author, and slides array). Each slide has a title and bullet points.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":     map[string]interface{}{"type": "string", "description": "Presentation title"},
				"author":    map[string]interface{}{"type": "string", "description": "Author name"},
				"slides":    map[string]interface{}{"type": "array", "description": "Array of slide objects, each with 'title' and 'bullets' (array of strings)"},
				"outputDir": map[string]interface{}{"type": "string", "description": "Output directory (default: current dir)"},
				"theme":     map[string]interface{}{"type": "string", "description": "Theme: default, dark, minimal (default: default)"},
			},
			"required": []string{"title", "slides"},
		},
		Permission: "write",
		HasEffects: true,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("presentation extension starting")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": agentDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "generate_presentation":
			result = generatePresentation(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func generatePresentation(ctx context.Context, args map[string]interface{}) callToolResult {
	title, _ := args["title"].(string)
	if title == "" {
		return callToolResult{Error: "title is required"}
	}
	author, _ := args["author"].(string)
	if author == "" {
		author = "Mneme"
	}
	outputDir := "."
	if d, ok := args["outputDir"].(string); ok && d != "" {
		outputDir = d
	}

	slidesRaw, ok := args["slides"].([]interface{})
	if !ok || len(slidesRaw) == 0 {
		return callToolResult{Error: "slides array is required with at least one slide"}
	}

	var slides []slide
	for _, s := range slidesRaw {
		if m, ok := s.(map[string]interface{}); ok {
			sl := slide{}
			if t, ok := m["title"].(string); ok {
				sl.Title = t
			}
			if bs, ok := m["bullets"].([]interface{}); ok {
				for _, b := range bs {
					if str, ok := b.(string); ok {
						sl.Bullets = append(sl.Bullets, str)
					}
				}
			}
			slides = append(slides, sl)
		}
	}

	os.MkdirAll(outputDir, 0755)
	filename := filepath.Join(outputDir, sanitizeFilename(title)+".pptx")

	if err := writePPTX(filename, title, author, slides); err != nil {
		return callToolResult{Error: fmt.Sprintf("generate pptx: %v", err)}
	}

	abs, err := filepath.Abs(filename)
	if err != nil {
		abs = filename
	}
	return callToolResult{Success: true, Output: fmt.Sprintf("Presentation generated: %s\nTitle: %s\nSlides: %d", abs, title, len(slides))}
}

// writePPTX creates a minimal .pptx file using the Open XML format.
// This produces a valid .pptx that can be opened by PowerPoint, LibreOffice, Google Slides.
func writePPTX(path, title, author string, slides []slide) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	// [Content_Types].xml
	writeZipFile(w, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
  <Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
  <Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
  <Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>
  <Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>
</Types>`)

	// _rels/.rels
	writeZipFile(w, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>`)

	// ppt/_rels/presentation.xml.rels
	relsXML := `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>`
	for i := range slides {
		relsXML += fmt.Sprintf(`
  <Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, i+3, i+1)
	}
	relsXML += "\n</Relationships>"
	writeZipFile(w, "ppt/_rels/presentation.xml.rels", relsXML)

	// ppt/presentation.xml
	presXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>
  <p:sldIdLst>`)
	for i := range slides {
		presXML += fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+3)
	}
	presXML += `</p:sldIdLst><p:defaultTextStyle/></p:presentation>`
	writeZipFile(w, "ppt/presentation.xml", presXML)

	// ppt/slideMasters/slideMaster1.xml
	writeZipFile(w, "ppt/slideMasters/slideMaster1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<p:sldMaster xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr/><p:grpSpPr/></p:spTree></p:cSld><p:layoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:layoutIdLst></p:sldMaster>`)

	// ppt/slideMasters/_rels/slideMaster1.xml.rels
	writeZipFile(w, "ppt/slideMasters/_rels/slideMaster1.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`)

	// ppt/slideLayouts/slideLayout1.xml
	writeZipFile(w, "ppt/slideLayouts/slideLayout1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Title and Body"><p:spTree><p:nvGrpSpPr/><p:grpSpPr/></p:spTree></p:cSld></p:sldLayout>`)

	// ppt/slideLayouts/_rels/slideLayout1.xml.rels
	writeZipFile(w, "ppt/slideLayouts/_rels/slideLayout1.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`)

	// ppt/theme/theme1.xml (minimal)
	writeZipFile(w, "ppt/theme/theme1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Default"><a:themeElements><a:clrScheme name="Default"><a:dk1/><a:lt1/><a:dk2/><a:lt2/><a:accent1/><a:accent2/><a:accent3/><a:accent4/><a:accent5/><a:accent6/></a:clrScheme></a:themeElements></a:theme>`)

	// Individual slide files
	for i, sl := range slides {
		slideNum := i + 1
		slideXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:cSld><p:spTree><p:nvGrpSpPr/><p:grpSpPr/>
    <p:sp><p:nvSpPr><p:cNvPr id="1" name="Title"/><p:cNvSpPr><a:spLocks noGrp="true"/></p:cNvSpPr><p:nvPr/></p:nvSpPr>
      <p:spPr><a:xfrm><a:off x="685800" y="274320"/><a:ext cx="8229600" cy="1143000"/></a:xfrm></p:spPr>
      <p:txBody><a:bodyPr/><a:lstStyle/>
        <a:p><a:r><a:rPr lang="en-US" sz="3200" b="1"/><a:t>%s</a:t></a:r></a:p>
      </p:txBody></p:sp>`, escapeXML(sl.Title))

		bodyY := 1600000
		for j, bullet := range sl.Bullets {
			slideXML += fmt.Sprintf(`
    <p:sp><p:nvSpPr><p:cNvPr id="%d" name="Bullet"/><p:cNvSpPr><a:spLocks noGrp="true"/></p:cNvSpPr><p:nvPr/></p:nvSpPr>
      <p:spPr><a:xfrm><a:off x="914400" y="%d"/><a:ext cx="7772400" cy="514350"/></a:xfrm></p:spPr>
      <p:txBody><a:bodyPr/><a:lstStyle/>
        <a:p><a:r><a:rPr lang="en-US" sz="2400"/><a:t>• %s</a:t></a:r></a:p>
      </p:txBody></p:sp>`, j+2, bodyY, escapeXML(bullet))
			bodyY += 550000
		}

		slideXML += `</p:spTree></p:cSld></p:sld>`

		writeZipFile(w, fmt.Sprintf("ppt/slides/slide%d.xml", slideNum), slideXML)
		writeZipFile(w, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", slideNum),
			`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`)
	}

	// Write author into docProps for metadata.
	writeZipFile(w, "docProps/core.xml", fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:creator>%s</dc:creator>
  <dc:title>%s</dc:title>
</cp:coreProperties>`, escapeXML(author), escapeXML(title)))
	return nil
}

func writeZipFile(w *zip.Writer, name string, content string) error {
	writer, err := w.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, content)
	return err
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_\-. ]+`)

func sanitizeFilename(s string) string {
	return strings.TrimSpace(sanitizeRe.ReplaceAllString(s, "_"))
}
