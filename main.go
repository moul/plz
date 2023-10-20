package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/mattn/go-isatty"
	"github.com/tidwall/pretty"
	"moul.io/http2curl"
)

const usage = "plz [OPTS] PROMPT..."

type config struct {
	apiBase string
	apiKey  string
	force   bool
	prompt  string
	model   string
	quiet   bool
	debug   bool
}

func main() {
	err := doMain(os.Args)
	switch err {
	case nil:
		// continue
	case flag.ErrHelp:
		fmt.Fprintf(os.Stderr, usage, err)
		flag.Usage()
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func doMain(args []string) error {
	cfg := config{}
	flag.StringVar(&cfg.apiBase, "api-base", "https://api.openai.com/v1", "API base URL")
	flag.StringVar(&cfg.apiKey, "api-key", os.Getenv("OPENAI_APIKEY"), "API key ($OPENAI_APIKEY)")
	flag.StringVar(&cfg.model, "model", "gpt-3.5-turbo-instruct", "Model to use")
	flag.BoolVar(&cfg.force, "f", false, "Run the generated program without asking for confirmation")
	flag.BoolVar(&cfg.quiet, "q", false, "Minimal output")
	flag.BoolVar(&cfg.debug, "debug", false, "Additional debug")
	flag.Parse()
	cfg.prompt = strings.TrimSpace(strings.Join(flag.Args(), " "))
	if cfg.prompt == "" {
		return flag.ErrHelp
	}
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		cfg.quiet = true
	}

	start := time.Now()
	if cfg.debug {
		defer func() {
			log.Println("execution time: ", time.Since(start))
		}()
	}

	s := spinner.New(spinner.CharSets[14], 100)
	s.Start()
	if cfg.quiet {
		s.Stop()
	}
	defer s.Stop()

	client := &http.Client{}
	requestBody, _ := json.Marshal(map[string]interface{}{
		"top_p":             1,
		"stop":              "```",
		"temperature":       0,
		"suffix":            "\n```",
		"max_tokens":        1000,
		"presence_penalty":  0,
		"frequency_penalty": 0,
		"model":             cfg.model,
		"prompt":            buildPrompt(cfg.prompt),
	})
	apiAddr := fmt.Sprintf("%s/completions", cfg.apiBase)
	req, _ := http.NewRequest("POST", apiAddr, bytes.NewBuffer(requestBody))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.apiKey))
	req.Header.Set("Content-Type", "application/json")
	if cfg.debug {
		debug, _ := http2curl.GetCurlCommand(req)
		fmt.Println(debug)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	if resp.StatusCode >= 400 {
		fmt.Println(color.RedString("API error: %v", err))
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("http code>=400: %s", pretty.Color(body, nil))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	code := strings.TrimSpace(result["choices"].([]interface{})[0].(map[string]interface{})["text"].(string))

	s.Stop()
	if !cfg.quiet {
		fmt.Println(color.GreenString("Got some code!"))
	}
	if !cfg.quiet || !cfg.force {
		fmt.Println(color.RedString(code))
	}

	shouldRun := cfg.force
	if !shouldRun {
		prompt := promptui.Select{
			Label: "Run the generated program? [Y/n]",
			Items: []string{"Yes", "No"},
		}

		_, result, err := prompt.Run()
		if err != nil {
			return fmt.Errorf("prompt failed: %v", err)
		}

		shouldRun = result == "Yes"
	}

	if shouldRun {
		s = spinner.New(spinner.CharSets[14], 100)
		s.Start()
		if cfg.quiet {
			s.Stop()
		}
		defer s.Stop()

		cmd := exec.Command("bash", "-c", code)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to execute the generated program: %v", err)
		}

		if cmd.ProcessState.ExitCode() != 0 {
			return fmt.Errorf("the program threw an error:\n%s", string(output))
		}

		s.Stop()
		if !cfg.quiet {
			fmt.Println(color.GreenString("Command ran successfully"))
		}
		fmt.Println(string(output))
	}

	return nil
}

func buildPrompt(prompt string) string {
	osHint := ""
	if strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") {
		osHint = " (on Windows)"
	} else if strings.Contains(strings.ToLower(os.Getenv("OS")), "darwin") {
		osHint = " (on macOS)"
	} else {
		osHint = " (on Linux)"
	}

	return fmt.Sprintf("%s%s:\n```bash\n#!/bin/bash\n", prompt, osHint)
}
