import os
import sys
from openai import OpenAI

NVIDIA_BASE_URL = "https://integrate.api.nvidia.com/v1"
MODEL = "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning"

def query_nemotron(prompt, temperature=0.6, top_p=0.95, max_tokens=65536, reasoning_budget=16384):
    api_key = os.getenv("NVIDIA_API_KEY")
    if not api_key:
        print("ERROR: NVIDIA_API_KEY environment variable not set", file=sys.stderr)
        sys.exit(1)

    client = OpenAI(base_url=NVIDIA_BASE_URL, api_key=api_key)

    completion = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": prompt}],
        temperature=temperature,
        top_p=top_p,
        max_tokens=max_tokens,
        extra_body={
            "chat_template_kwargs": {"enable_thinking": True},
            "reasoning_budget": reasoning_budget,
        },
        stream=False,
    )

    reasoning = getattr(completion.choices[0].message, "reasoning_content", None)
    if reasoning:
        sys.stdout.reconfigure(encoding='utf-8')
        print(reasoning)
    sys.stdout.reconfigure(encoding='utf-8')
    print(completion.choices[0].message.content)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        prompt = sys.stdin.read().strip()
        if not prompt:
            print("Usage: python nvidia_nemotron.py <prompt>", file=sys.stderr)
            print("   or: echo <prompt> | python nvidia_nemotron.py", file=sys.stderr)
            sys.exit(1)
    else:
        prompt = " ".join(sys.argv[1:])

    query_nemotron(prompt)
