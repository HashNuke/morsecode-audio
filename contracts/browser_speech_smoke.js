// Run in a browser page on a different origin using agent-browser eval --stdin.
// Set window.morseApiBaseUrl explicitly first. No real credentials are used.
(async () => {
  const base = new URL(window.morseApiBaseUrl);
  if (!['http:', 'https:'].includes(base.protocol)) throw new Error('Explicit HTTP API origin required');
  const wsBase = new URL(base);
  wsBase.protocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
  const issue = async purpose => {
    const response = await fetch(new URL(`/v1/single-use-token/${purpose}`, base), {
      method: 'POST', headers: { 'xi-api-key': 'synthetic-browser-issuer' }, credentials: 'omit',
    });
    if (!response.ok) throw new Error(`Token issuance failed: ${response.status}`);
    return (await response.json()).token;
  };
  const ttsResults = [];
  for (const mode of ['initial-key', 'token']) {
    const endpoint = new URL('/v1/text-to-speech/morse/stream-input', wsBase);
    endpoint.searchParams.set('output_format', 'pcm_16000');
    if (mode === 'token') endpoint.searchParams.set('single_use_token', await issue('tts_websocket'));
    const result = await new Promise((resolve, reject) => {
      const socket = new WebSocket(endpoint);
      const timer = setTimeout(() => { socket.close(); reject(new Error('TTS timeout')); }, 5000);
      let bytes = 0, finals = 0, text = '';
      socket.onopen = () => {
        socket.send(JSON.stringify({ text: ' ', ...(mode === 'initial-key' ? { xi_api_key: 'synthetic-browser-key' } : {}) }));
        socket.send(JSON.stringify({ text: 'E T', flush: true }));
        socket.send(JSON.stringify({ text: '' }));
      };
      socket.onmessage = event => {
        const message = JSON.parse(event.data);
        if (message.error) { clearTimeout(timer); socket.close(); reject(new Error('TTS protocol failure')); return; }
        if (message.isFinal) finals++;
        if (message.audio) bytes += atob(message.audio).length;
        if (message.normalizedAlignment) text += message.normalizedAlignment.chars.join('');
      };
      socket.onerror = () => { clearTimeout(timer); reject(new Error('TTS transport failure')); };
      socket.onclose = event => {
        clearTimeout(timer);
        if (event.code !== 1000 || bytes !== 48000 || finals !== 1 || text !== 'E T') reject(new Error('TTS result mismatch'));
        else resolve({ mode, bytes, finals, closeCode: event.code });
      };
    });
    ttsResults.push(result);
  }
  const multiUrl = new URL('/v1/text-to-speech/morse/multi-stream-input', wsBase);
  multiUrl.searchParams.set('output_format', 'pcm_16000');
  multiUrl.searchParams.set('auto_mode', 'true');
  multiUrl.searchParams.set('single_use_token', await issue('tts_websocket'));
  const multi = await new Promise((resolve, reject) => {
    const socket = new WebSocket(multiUrl);
    const timer = setTimeout(() => { socket.close(); reject(new Error('Multi TTS timeout')); }, 5000);
    const bytes = { old: 0, next: 0 }, finals = { old: 0, next: 0 };
    let interrupted = false;
    socket.onopen = () => socket.send(JSON.stringify({ context_id: 'old', text: 'E' }));
    socket.onmessage = event => {
      const message = JSON.parse(event.data), id = message.contextId;
      if (message.error || !(id in bytes)) { clearTimeout(timer); socket.close(); reject(new Error('Multi protocol failure')); return; }
      if (message.isFinal) finals[id]++;
      if (message.audio) {
        if (finals[id]) { socket.close(); reject(new Error('Stale multi audio')); return; }
        bytes[id] += atob(message.audio).length;
      }
      if (id === 'old' && message.audio && !interrupted) {
        interrupted = true;
        socket.send(JSON.stringify({ context_id: 'old', close_context: true }));
        socket.send(JSON.stringify({ context_id: 'next', text: 'T', flush: true }));
      }
      if (finals.old === 1 && finals.next === 1) socket.send(JSON.stringify({ close_socket: true }));
    };
    socket.onerror = () => { clearTimeout(timer); reject(new Error('Multi transport failure')); };
    socket.onclose = event => {
      clearTimeout(timer);
      if (event.code !== 1000 || bytes.old !== 1920 || bytes.next !== 32640 || finals.old !== 1 || finals.next !== 1) reject(new Error('Multi result mismatch'));
      else resolve({ bytes, finals, closeCode: event.code });
    };
  });
  const token = await issue('realtime_scribe');
  const endpoint = new URL('/v1/speech-to-text/realtime', wsBase);
  endpoint.searchParams.set('token', token);
  const transcript = await new Promise((resolve, reject) => {
    const socket = new WebSocket(endpoint);
    const timer = setTimeout(() => { socket.close(); reject(new Error('STT timeout')); }, 5000);
    let final = '';
    socket.onmessage = event => {
      const message = JSON.parse(event.data);
      if (message.message_type === 'session_started') {
        const pcm = new Uint8Array(28800), samples = new DataView(pcm.buffer);
        for (let i = 0; i < 960; i++) samples.setInt16(i * 2, Math.floor(i * 1400 / 16000) % 2 ? -3000 : 3000, true);
        socket.send(JSON.stringify({ message_type: 'input_audio_chunk', audio_base_64: btoa(String.fromCharCode(...pcm)), sample_rate: 16000, commit: true }));
      } else if (message.message_type === 'committed_transcript') {
        final = message.text;
        socket.close(1000);
      }
    };
    socket.onerror = () => { clearTimeout(timer); reject(new Error('STT transport failure')); };
    socket.onclose = () => { clearTimeout(timer); if (final === 'E') resolve(final); else reject(new Error('STT result mismatch')); };
  });
  return { browser: navigator.userAgent, pageOrigin: location.origin, apiOrigin: base.origin, tts: ttsResults, multi, transcript };
})();
