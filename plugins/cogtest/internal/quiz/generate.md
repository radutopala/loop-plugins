You are writing a short multiple-choice quiz that checks whether the developer of this project still understands its code. The project is the channel "{{.ChannelName}}"; its files are in your working directory.

## Read first

Read these files, and what they call into when you need it, before writing any question:

{{range .Focus}}- `{{.Path}}`: {{.Reason}}
{{end}}
## Write {{.Count}} questions

Rules for every question:

- Ask about behaviour, design decisions, data flow, invariants, error handling or edge cases a developer working on this code should know. No trivia: no line numbers, exact spellings, counts or formatting.
- Base it on code you read in this session. Never guess.
- Give 3 to 5 choices, all different and plausible. Exactly one is correct. No "all of the above", "none of the above" or the like.
- `correct_index` is the 0-based index of the correct choice. Vary its position across questions.
- `explanation`: 1 to 3 sentences on why the correct choice is right and the tempting ones wrong. It must mention the path of at least one of the question's anchors, for example "`internal/store/store.go` retries ...".
- `anchors`: 1 to 3 places in the code that prove the answer, each a repo-relative `path` with a 1-based inclusive line range `start`..`end` of at most {{.MaxAnchorLines}} lines that exists in the file now.
- Each question covers something different; spread them across the files above.
{{- if .Avoid}}

Do not repeat or rephrase these earlier questions:

{{range .Avoid}}- {{.}}
{{end}}{{end}}

## Submit

Call the `plugin_submit_questions` tool once with all {{.Count}} questions:

```json
{"questions": [{"question": "...", "choices": ["...", "...", "..."], "correct_index": 1, "explanation": "...", "anchors": [{"path": "dir/file.go", "start": 10, "end": 24}]}]}
```

If the tool answers with errors, nothing was saved: fix every problem it lists and call it again with all {{.Count}} questions. When it answers that the quiz is saved, stop. Do not edit any file and do not write the questions anywhere else.
