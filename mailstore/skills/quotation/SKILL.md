---
name: quotation
description: Verify sources and prepare a complete commercial document draft.
---

DOCUMENT COMPLETENESS (QUOTATIONS & CONTRACTS):
- A document is filled from the WHOLE thread, not from the user's last sentence. Re-read the conversation before calling generate_document and collect: the buyer's full company name, contact person, email, phone and address; the customer's PO or reference number; the currency; every product model, description and quantity discussed; payment terms, lead time, validity and any delivery or packing note.
- Buyer details come from the thread itself. The sender's signature usually carries the company name, phone and address, and the From/Cc headers carry the email address. Copy the company name exactly as it is written. Never leave the buyer blank when anyone in the thread names the company.
- Line items: ONE ROW PER PRODUCT. If the mail lists or tabulates products, reproduce every row in the same order with model, description, quantity and unit price, and let the amount be quantity x price. Never collapse several products into one summary row, and never drop a row the thread discusses.
- Fill fields only from verified sources or explicit user input. Preserve source values and units, use calculate_pricing for amounts, and leave unsupported fields empty while identifying the missing information.
- Everything without a dedicated argument goes into "values", keyed by these concept ids: customer_po, validity, commercial_validity, lead_time, payment_terms, trade_term, origin, port_of_loading, remarks, notes, item_model, contract_title, seller_company, seller_contact, seller_email, seller_phone, seller_address, bank_account_name, bank_account_no, bank_swift, bank_name, bank_address, bank_country, payment_method, order_note_1, order_note_2, order_note_3, order_note_4. Fill seller and bank details only when the thread or the user's own signature states them; never invent a bank account.
- A field the document needs but the thread does not contain stays empty AND is named in your reply, so the user can supply it. Do not silently hand over a half-empty document.
