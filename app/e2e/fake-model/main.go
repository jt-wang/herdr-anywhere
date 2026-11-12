// A stand-in for a model behind an OpenAI-compatible chat completions API, so Oh My Pi can be run in the e2e with no
// model and no key: the first turn answers with a call of the "read" tool on the path given by -read, the turn after
// the tool's result with one line of text. Every request is logged (tools named, roles) to stderr.
//
//	fake-model -addr 127.0.0.1:8099 -read docs/chart.png
//	fake-model -png docs/chart.png   writes a made-up bar chart (240x160)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "listen address")
	read := flag.String("read", "docs/chart.png", "path the read tool call asks for")
	pngOut := flag.String("png", "", "write a made-up chart PNG here and exit")
	flag.Parse()
	if *pngOut != "" {
		if err := chart(*pngOut); err != nil {
			log.Fatal(err)
		}
		return
	}
	http.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"test-model","object":"model","owned_by":"local"}]}`)
	})
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		roles, tools := []string{}, []string{}
		answered := false
		for _, m := range req.Messages {
			roles = append(roles, m.Role)
			if m.Role == "tool" {
				answered = true
			}
		}
		for _, t := range req.Tools {
			tools = append(tools, t.Function.Name)
		}
		log.Printf("request stream=%v roles=%s tools=%s", req.Stream, strings.Join(roles, ","), strings.Join(tools, ","))
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		chunk := func(delta map[string]any, finish any) map[string]any {
			return map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "test-model",
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		}
		if answered {
			send(chunk(map[string]any{"role": "assistant", "content": "That is the chart."}, nil))
			send(chunk(map[string]any{}, "stop"))
		} else {
			args, _ := json.Marshal(map[string]string{"path": *read})
			send(chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": "read", "arguments": string(args)}}}}, nil))
			send(chunk(map[string]any{}, "tool_calls"))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// chart draws four bars on a light ground: made up, nothing of anyone's.
func chart(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 240, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{0xf4, 0xf1, 0xea, 0xff})
		}
	}
	bars := []struct {
		h int
		c color.RGBA
	}{{60, color.RGBA{0x2f, 0x6f, 0xb3, 0xff}}, {110, color.RGBA{0xd9, 0x6c, 0x2b, 0xff}}, {85, color.RGBA{0x3c, 0x9a, 0x5f, 0xff}}, {135, color.RGBA{0x8a, 0x4f, 0xb8, 0xff}}}
	for i, b := range bars {
		for y := 150 - b.h; y < 150; y++ {
			for x := 20 + i*55; x < 20+i*55+40; x++ {
				img.Set(x, y, b.c)
			}
		}
	}
	for x := 10; x < 230; x++ {
		img.Set(x, 150, color.RGBA{0x33, 0x33, 0x33, 0xff})
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
