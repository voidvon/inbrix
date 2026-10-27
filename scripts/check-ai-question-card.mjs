import assert from 'node:assert/strict';
import { createServer } from 'vite';
import { chromium } from 'playwright';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('../frontend/src/lib/ai-chat-parts.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { getAIChatQuestion } = await import(`data:text/javascript;base64,${Buffer.from(compiled.outputText).toString('base64')}`);
const question = { question: '请选择数量', options: ['10件', '20件'] };
const messages = [{ role: 'assistant', content: question.question, question, toolCalls: [{ name: 'ask_user', status: 'success' }] }];
assert.deepEqual(getAIChatQuestion(JSON.parse(JSON.stringify(messages))), question);
assert.equal(getAIChatQuestion([...messages, { role: 'user', content: '20件' }]), undefined);
assert.deepEqual(getAIChatQuestion([{ ...messages[0], question: undefined }]), { question: question.question, options: [] });

const server = await createServer({ configFile: 'frontend/vite.config.ts', server: { host: '127.0.0.1', port: 0, strictPort: false }, plugins: [{
  name: 'question-card-check',
  configureServer(vite) {
    vite.middlewares.use('/__question-card', async (_req, res) => {
      res.setHeader('Content-Type', 'text/html');
      res.end(await vite.transformIndexHtml('/__question-card', `<!doctype html><html><body><div id="root"></div><output id="answer"></output><script type="module">
        import React from 'react';
        import { createRoot } from 'react-dom/client';
        import { AIQuestionCard } from '/src/components/app/ai-question-card.tsx';
        import '/src/styles.css';
        createRoot(document.getElementById('root')).render(React.createElement(AIQuestionCard, {
          question: ${JSON.stringify(question)}, isZh: true, disabled: false,
          onSubmit: answer => document.getElementById('answer').textContent = answer
        }));
      </script></body></html>`));
    });
  },
}] });
let browser;
try {
  await server.listen();
  const address = server.httpServer.address();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  await page.goto(`http://127.0.0.1:${address.port}/__question-card`);
  await page.getByTestId('ai-question-card').waitFor();
  const submit = page.getByRole('button', { name: '提交并继续' });
  assert.equal(await submit.isDisabled(), true);
  assert.equal(await page.getByRole('radio').count(), 3);
  assert.equal(await page.getByRole('textbox').count(), 0);
  await page.getByRole('radio', { name: '20件', exact: true }).check();
  await submit.click();
  assert.equal(await page.locator('#answer').textContent(), '20件');
  await page.getByRole('radio', { name: '其他，自行输入' }).check();
  assert.equal(await submit.isDisabled(), true);
  await page.getByRole('textbox', { name: '你的答案' }).fill('  需要35件  ');
  await submit.click();
  assert.equal(await page.locator('#answer').textContent(), '需要35件');
  await page.getByRole('radio', { name: '10件', exact: true }).check();
  assert.equal(await page.getByRole('textbox').count(), 0);
  assert.equal(await page.getByRole('radio', { checked: true }).count(), 1);
  console.log('Passed: persisted questions, answered state, single selection, custom input, and dedicated form submission.');
} finally {
  await browser?.close();
  await server.close();
}
