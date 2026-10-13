/**
 * Demo affordances for local development.
 *
 * The server only issues the fixed OTP when APP_DEBUG is set, but the built
 * bundle still shipped the demo phone numbers and the code to every visitor.
 * These constants are empty in a production build, so the literals are dead
 * code that the minifier drops, and the sign-in screen has nothing to show.
 */

const isDev = import.meta.env.DEV

/** A phone number to prefill while developing, or '' in production. */
export const DEMO_PHONE = isDev ? '+15551111111' : ''

/** The demo code, for the placeholder on the code step. Production shows ''. */
export const DEMO_CODE = isDev ? '123456' : ''

/** Instructions for the tester. Empty in production. */
export const DEMO_HINT = isDev
  ? 'Demo: Alex +15551111111 · Jordan +15552222222 · code 123456. Apple/Google are local stand-ins until real keys are set.'
  : ''

/** True when the local-only OAuth stand-ins should be offered. */
export const SHOW_DEMO_OAUTH = isDev
