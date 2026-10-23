import assert from 'node:assert/strict';

// In-memory API fixture for browser tests. UI must send a single new user turn;
// only the simulated server owns message history and attachment references.
export function createAIChatTestStore(accountEmail, businessContext = { agentId: '', skillId: '', brands: [] }) {
  let counter = 0;
  const runs = new Map();
  const sessions = new Map();
  const make = (id, kind = 'document', title = 'Document Assistant', documentType = 'quotation') => {
    const now = new Date().toISOString();
    const session = { id, kind, title, documentType, version: 0, messages: [], businessContext: kind === "mail" ? { ...businessContext, brands: [...businessContext.brands] } : { agentId: "", skillId: "", brands: [] }, createdAt: now, updatedAt: now };
    sessions.set(id, session); return session;
  };
  const mail = make('mail-session', 'mail');
  const resolveRetry = payload => {
    if (!payload.retryRequestId) return payload;
    const run = runs.get(payload.retryRequestId); assert.ok(run, 'Recover a saved run');
    assert.equal(run.payload.sessionId, payload.sessionId);
    assert.equal(run.payload.accountEmail, payload.accountEmail);
    return structuredClone(run.payload);
  };
  const begin = payload => {
    payload = resolveRetry(payload);
    assert.equal(payload.accountEmail, accountEmail);
    assert.equal(payload.messages.length, 1, 'Client sends only the new user message');
    assert.equal(payload.messages[0].role, 'user');
    assert.ok(payload.requestId);
    const session = sessions.get(payload.sessionId); assert.ok(session, 'Session must exist');
    let run = runs.get(payload.requestId);
    if (!run) {
      assert.equal(payload.version, session.version, 'Client uses current session version');
      const user = { ...payload.messages[0], id: `user-${++counter}`, createdAt: new Date().toISOString() };
      session.messages.push(user); session.version++;
      run = { user, payload: structuredClone(payload) }; runs.set(payload.requestId, run);
    } else assert.deepEqual(payload, run.payload, 'Retries preserve the entire original request');
    session.lastRun = { requestId: payload.requestId, status: 'pending', userMessageId: run.user.id, retryable: false };
    return { session, run };
  };
  const complete = (payload, response) => {
    payload = resolveRetry(payload);
    const { session, run } = begin(payload);
    if (run.response) return run.response;
    const id = `assistant-${++counter}`;
    session.version++;
    if (response.businessContext) session.businessContext = response.businessContext;
    session.messages.push({ ...response, id, role: 'assistant', createdAt: new Date().toISOString() });
    if (response.document) session.currentDocument = structuredClone(response.document);
    session.lastRun.status = 'complete';
    run.response = { ...response, sessionId: session.id, requestId: payload.requestId, version: session.version, userMessageId: run.user.id, assistantMessageId: id };
    return run.response;
  };
  const fail = payload => { const { session } = begin(payload); session.lastRun.status = 'failed'; session.lastRun.retryable = true; };
  const projectSession = (session, url) => {
    const limit = Number(url.searchParams.get('limit') || 100);
    const before = Number(url.searchParams.get('before') ?? session.messages.length);
    const start = Math.max(0, before - limit);
    return { ...session, messages: session.messages.slice(start, before), hasMoreMessages: start > 0, ...(start > 0 ? { nextBefore: start } : {}) };
  };
  const routeSessions = (req, url) => {
    if (url.pathname === '/api/ai/chat-sessions') {
      if (req.method() === 'GET') {
        const rows = [...sessions.values()].filter(s => s.kind === 'document');
        const start = Number(url.searchParams.get('cursor') || 0), limit = Number(url.searchParams.get('limit') || 30);
        return { json: { sessions: rows.slice(start, start + limit).map(s => ({ ...s, messages: [] })), ...(start + limit < rows.length ? { nextCursor: String(start + limit) } : {}) } };
      }
      const payload = req.postDataJSON(); assert.equal(payload.accountEmail, accountEmail);
      return { status: 201, json: make(`document-${++counter}`, 'document', payload.title, payload.documentType) };
    }
    if (!url.pathname.startsWith('/api/ai/chat-sessions/')) return null;
    const id = url.pathname.split('/').at(-1), session = sessions.get(id); assert.ok(session);
    assert.equal(url.searchParams.get('accountEmail'), accountEmail);
    if (req.method() === 'DELETE') { assert.equal(Number(url.searchParams.get('version')), session.version); sessions.delete(id); return { status: 204 }; }
    if (req.method() === 'PATCH') {
      const body = req.postDataJSON(); assert.equal(body.version, session.version);
      session.version++; session.businessContext = body.businessContext;
      if (session.lastRun) session.lastRun.retryable = false;
    }
    return { json: projectSession(session, url) };
  };
  return { mail, sessions, begin, complete, fail, routeSessions, resolveRetry, projectSession };
}
