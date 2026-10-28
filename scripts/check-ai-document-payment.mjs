import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Uses only the temporary database in product-library-harness.go.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3219';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
  await page.goto(`${base}/documents`);
  const records = await page.evaluate(async () => {
    const { convertGeneratedToStoredDocument } = await import('/src/lib/ai-document-storage.ts');
    const { createBusinessFromTemplate, projectDocumentBusiness } = await import('/src/lib/document-business.ts');
    const { saveDocument, getDocument } = await import('/src/lib/api.ts');
    const defaults = {
      bank_account_name: 'SHANGHAI SPIRAXSARCO FLUID EQUIPMENT CO., LTD',
      bank_account_no: 'LU024080000056414710', bank_swift: 'BCIRLULL (or BCIRLULLXXX)',
      bank_name: 'Banking Circle S.A', bank_address: '2 Boulevard de la Foire L-1528 Luxembourg',
      bank_country: 'Luxembourg', bank_account_type: 'Business Account',
      payment_method: 'For the payment of goods, please make a SWIFT(Wire/TT) Payment',
      payment_memo: 'Test Buyer / PI-PAYMENT-1 / Valve',
    };
    const records = [];
    // Both paths use the built-in renderer; populated HTML uses a server-side
    // template and is covered by TestGeneratedContractInheritsTemplatePaymentDetails.
    for (const mode of ['no-html', 'marker', 'overrides', 'clear']) {
      const overrides = mode === 'overrides'
        ? Object.fromEntries(Object.keys(defaults).map(key => [key, `Custom ${key}`]))
        : mode === 'clear' ? Object.fromEntries(Object.keys(defaults).map(key => [key, ''])) : {};
      const expected = { ...defaults, ...overrides };
      const generated = {
        id: crypto.randomUUID(), type: 'contract', name: `Payment regression ${crypto.randomUUID()}`,
        html: mode === 'marker' ? '<div data-spirax-contract="reference-v1"></div>' : '',
        values: { contract_number: 'PI-PAYMENT-1', customer_company: 'Test Buyer', currency: 'USD', ...overrides },
        items: [{ model: 'Valve', description: 'Valve', qty: '1', price: '100', amount: '100' }],
      };
      const draft = convertGeneratedToStoredDocument(generated);
      const payload = JSON.parse(draft.html.split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
      const check = (actual, label) => {
        for (const [key, value] of Object.entries(expected)) {
          if (actual[key] !== value) throw new Error(`${mode} ${label}: ${key}=${JSON.stringify(actual[key])}, expected ${JSON.stringify(value)}`);
        }
      };
      check(payload.business.values, 'generated snapshot');
      // Opening an editor always projects the canonical snapshot onto controls.
      projectDocumentBusiness(payload.data, payload.business);
      check(createBusinessFromTemplate(payload.data).values, 'projected controls');
      const { document: saved } = await saveDocument({ ...draft, html: '__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify(payload) });
      const { document: reopened } = await getDocument(saved.id);
      const stored = JSON.parse(reopened.html.split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
      check(stored.business.values, 'reopened snapshot');
      check(createBusinessFromTemplate(stored.data).values, 'reopened controls');
      records.push({ id: saved.id, expected });
    }
    return records;
  });
  for (const record of records) {
    await page.goto(`${base}/documents/${record.id}`);
    const dialog = page.getByTestId('document-editor-dialog');
    await dialog.locator('canvas').first().waitFor();
    await page.waitForFunction(() => {
      const host = document.querySelector('.canvas-document-editor');
      let fiber = host?.[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
      for (; fiber; fiber = fiber.return) {
        if (fiber.ref?.current?.getControls) { window.paymentEditor = fiber.ref.current; return true; }
      }
      return false;
    });
    const controls = await page.evaluate(() => Object.fromEntries(window.paymentEditor.getControls().map(control => [control.conceptId, control.value || ''])));
    for (const [key, value] of Object.entries(record.expected)) assert.equal(controls[key], value, key);
  }
  assert.deepEqual(errors, []);
  console.log('PASS: all nine payment fields survive AI conversion, projection, save, reopen, and actual editor loading; custom values and explicit clearing are respected.');
} finally {
  await browser.close();
}
