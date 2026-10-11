// Mirror of the server's game registry protocols (game/registry.go). The
// client renders one component per protocol, and every protocol here must be
// implemented in App.tsx's RoundAnswers.

export type GameProtocol =
  | 'pick2'
  | 'pick4'
  | 'phased'
  | 'spectrum'
  | 'order'
  | 'stance'
  | 'coop'
  | 'rapid'
  | 'rate'
  | 'branch'

export interface GameItem {
  kind: string
  label: string
  protocol: GameProtocol
  rounds: number
  timerSec: number
  ready: boolean
}

export const gameProtocols: GameProtocol[] = [
  'pick2',
  'pick4',
  'phased',
  'spectrum',
  'order',
  'stance',
  'coop',
  'rapid',
  'rate',
  'branch',
]