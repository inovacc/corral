package airef

// Integration snippet templates, keyed by language. Placeholders: {{MODEL}},
// {{KEYENV}}, {{BASEURL}}. Keys are always read from the environment, never
// inlined. Official vendor SDKs are used where they exist; Rust uses REST.

var anthropicSnippets = map[string]string{
	"curl": `curl https://api.anthropic.com/v1/messages \
  --header "x-api-key: ${{KEYENV}}" \
  --header "anthropic-version: 2023-06-01" \
  --header "content-type: application/json" \
  --data '{"model":"{{MODEL}}","max_tokens":1024,"messages":[{"role":"user","content":"Hello, world"}]}'`,

	"go": `// go get github.com/anthropics/anthropic-sdk-go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/anthropics/anthropic-sdk-go"
)

func main() {
	client := anthropic.NewClient() // reads {{KEYENV}}
	resp, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     "{{MODEL}}",
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Hello, world")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, b := range resp.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			fmt.Println(t.Text)
		}
	}
}`,

	"python": `# pip install anthropic
import anthropic

client = anthropic.Anthropic()  # reads {{KEYENV}}
msg = client.messages.create(
    model="{{MODEL}}",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello, world"}],
)
print(msg.content[0].text)`,

	"typescript": `// npm i @anthropic-ai/sdk
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic(); // reads {{KEYENV}}
const msg = await client.messages.create({
  model: "{{MODEL}}",
  max_tokens: 1024,
  messages: [{ role: "user", content: "Hello, world" }],
});
console.log(msg.content[0].type === "text" ? msg.content[0].text : "");`,

	"javascript": `// npm i @anthropic-ai/sdk   (ESM: "type":"module")
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic(); // reads {{KEYENV}}
const msg = await client.messages.create({
  model: "{{MODEL}}",
  max_tokens: 1024,
  messages: [{ role: "user", content: "Hello, world" }],
});
console.log(msg.content[0].text);`,

	"java": `// build.gradle.kts: implementation("com.anthropic:anthropic-java:LATEST")
import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.models.messages.Message;
import com.anthropic.models.messages.MessageCreateParams;

AnthropicClient client = AnthropicOkHttpClient.fromEnv(); // reads {{KEYENV}}
MessageCreateParams params = MessageCreateParams.builder()
    .model("{{MODEL}}")
    .maxTokens(1024)
    .addUserMessage("Hello, world")
    .build();
Message msg = client.messages().create(params);
System.out.println(msg.content().get(0).text().orElseThrow().text());`,

	"rust": `// cargo add reqwest --features json && cargo add tokio --features full && cargo add serde_json
// No official Anthropic Rust SDK — REST via reqwest.
use serde_json::json;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let key = std::env::var("{{KEYENV}}")?;
    let body = json!({
        "model": "{{MODEL}}",
        "max_tokens": 1024,
        "messages": [{"role": "user", "content": "Hello, world"}]
    });
    let resp = reqwest::Client::new()
        .post("https://api.anthropic.com/v1/messages")
        .header("x-api-key", key)
        .header("anthropic-version", "2023-06-01")
        .json(&body)
        .send().await?
        .json::<serde_json::Value>().await?;
    println!("{}", resp["content"][0]["text"]);
    Ok(())
}`,
}

var openaiSnippets = map[string]string{
	"curl": `curl {{BASEURL}}/chat/completions \
  -H "Authorization: Bearer ${{KEYENV}}" \
  -H "Content-Type: application/json" \
  -d '{"model":"{{MODEL}}","messages":[{"role":"user","content":"Hello, world"}]}'`,

	"go": `// go get github.com/openai/openai-go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func main() {
	client := openai.NewClient(option.WithBaseURL("{{BASEURL}}")) // key from {{KEYENV}}
	resp, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model: "{{MODEL}}",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Hello, world"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Choices[0].Message.Content)
}`,

	"python": `# pip install openai
from openai import OpenAI

client = OpenAI(base_url="{{BASEURL}}")  # key from {{KEYENV}}
resp = client.chat.completions.create(
    model="{{MODEL}}",
    messages=[{"role": "user", "content": "Hello, world"}],
)
print(resp.choices[0].message.content)`,

	"typescript": `// npm i openai
import OpenAI from "openai";

const client = new OpenAI({ baseURL: "{{BASEURL}}" }); // key from {{KEYENV}}
const resp = await client.chat.completions.create({
  model: "{{MODEL}}",
  messages: [{ role: "user", content: "Hello, world" }],
});
console.log(resp.choices[0].message.content);`,

	"javascript": `// npm i openai   (ESM: "type":"module")
import OpenAI from "openai";

const client = new OpenAI({ baseURL: "{{BASEURL}}" }); // key from {{KEYENV}}
const resp = await client.chat.completions.create({
  model: "{{MODEL}}",
  messages: [{ role: "user", content: "Hello, world" }],
});
console.log(resp.choices[0].message.content);`,

	"java": `// build.gradle.kts: implementation("com.openai:openai-java:LATEST")
// For OpenRouter, use OpenAIOkHttpClient.builder().baseUrl("{{BASEURL}}").fromEnv().build()
import com.openai.client.OpenAIClient;
import com.openai.client.okhttp.OpenAIOkHttpClient;
import com.openai.models.chat.completions.ChatCompletion;
import com.openai.models.chat.completions.ChatCompletionCreateParams;

OpenAIClient client = OpenAIOkHttpClient.fromEnv(); // reads {{KEYENV}}
ChatCompletionCreateParams params = ChatCompletionCreateParams.builder()
    .model("{{MODEL}}")
    .addUserMessage("Hello, world")
    .build();
ChatCompletion c = client.chat().completions().create(params);
System.out.println(c.choices().get(0).message().content().orElse(""));`,

	"rust": `// cargo add reqwest --features json && cargo add tokio --features full && cargo add serde_json
// async-openai crate also works; REST via reqwest shown for portability.
use serde_json::json;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let key = std::env::var("{{KEYENV}}")?;
    let body = json!({"model": "{{MODEL}}", "messages": [{"role": "user", "content": "Hello, world"}]});
    let resp = reqwest::Client::new()
        .post("{{BASEURL}}/chat/completions")
        .bearer_auth(key)
        .json(&body)
        .send().await?
        .json::<serde_json::Value>().await?;
    println!("{}", resp["choices"][0]["message"]["content"]);
    Ok(())
}`,
}

var geminiSnippets = map[string]string{
	"curl": `curl "https://generativelanguage.googleapis.com/v1beta/models/{{MODEL}}:generateContent?key=${{KEYENV}}" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"parts":[{"text":"Hello, world"}]}]}'`,

	"go": `// go get google.golang.org/genai
package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{Backend: genai.BackendGeminiAPI}) // key from {{KEYENV}}
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.Models.GenerateContent(ctx, "{{MODEL}}", genai.Text("Hello, world"), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Text())
}`,

	"python": `# pip install google-genai
from google import genai

client = genai.Client()  # key from {{KEYENV}} (or GOOGLE_API_KEY)
resp = client.models.generate_content(model="{{MODEL}}", contents="Hello, world")
print(resp.text)`,

	"typescript": `// npm i @google/genai
import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({}); // key from {{KEYENV}} (or GEMINI_API_KEY)
const resp = await ai.models.generateContent({
  model: "{{MODEL}}",
  contents: "Hello, world",
});
console.log(resp.text);`,

	"javascript": `// npm i @google/genai   (ESM: "type":"module")
import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({}); // key from {{KEYENV}}
const resp = await ai.models.generateContent({
  model: "{{MODEL}}",
  contents: "Hello, world",
});
console.log(resp.text);`,

	"java": `// build.gradle.kts: implementation("com.google.genai:google-genai:LATEST")
import com.google.genai.Client;
import com.google.genai.types.GenerateContentResponse;

Client client = new Client(); // reads {{KEYENV}} / GOOGLE_API_KEY
GenerateContentResponse resp =
    client.models.generateContent("{{MODEL}}", "Hello, world", null);
System.out.println(resp.text());`,

	"rust": `// cargo add reqwest --features json && cargo add tokio --features full && cargo add serde_json
// No official Gemini Rust SDK — REST via reqwest.
use serde_json::json;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let key = std::env::var("{{KEYENV}}")?;
    let url = format!(
        "https://generativelanguage.googleapis.com/v1beta/models/{{MODEL}}:generateContent?key={}",
        key
    );
    let body = json!({"contents":[{"parts":[{"text":"Hello, world"}]}]});
    let resp = reqwest::Client::new().post(url).json(&body)
        .send().await?
        .json::<serde_json::Value>().await?;
    println!("{}", resp["candidates"][0]["content"]["parts"][0]["text"]);
    Ok(())
}`,
}
