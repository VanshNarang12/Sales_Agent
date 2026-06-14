// Client configuration. No secrets — the dev gateway runs with AUTH_DISABLED=true.
export const config = {
  // Realtime Gateway WebSocket endpoint.
  gatewayWsUrl: process.env.GATEWAY_WS_URL ?? "ws://localhost:8080/v1/realtime",
  // PCM frame size in samples sent per WS message.
  frameSize: 2048,
};
