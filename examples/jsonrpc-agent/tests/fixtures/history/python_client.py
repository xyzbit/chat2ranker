"""Run the shared imported-history scenario through the Python SDK."""
import json
import os
from deepseek_harness import DeepSeekHarness, DeepSeekHarnessConfig

with DeepSeekHarness(DeepSeekHarnessConfig(
    launch_args_override=tuple(json.loads(os.environ['HISTORY_LAUNCH'])),
    cwd=os.environ['DSH_CWD'],
    model='deepseek-v4-flash',
    request_timeout_seconds=45,
)) as harness:
    result = harness.run('Who did I mention?', session_id='history-python')
    imported = [e for e in result.events if e['type'] == 'history/message']
    print(json.dumps({
        'answer': result.final_response,
        'imported': [{'role': e['data']['message']['role'], 'text': e['data']['message']['content'][0]['text']} for e in imported],
        'nativeAnswers': sum(e['type'] == 'assistant/message' for e in result.events),
    }))
