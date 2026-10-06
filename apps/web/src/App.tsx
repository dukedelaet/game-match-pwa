import { useEffect, useState } from 'react'
import {
  Outlet,
  Link,
  RouterProvider,
  createRootRoute,
  createRoute,
  createRouter,
  useNavigate,
  redirect,
} from '@tanstack/react-router'
import { api, type Me } from './api'
import { useInstallPrompt } from './useInstallPrompt'

let meCache: Me | null = null

async function requireUser() {
  try {
    const { user } = await api.get('/v1/auth/session')
    meCache = user
    return user as Me
  } catch {
    meCache = null
    throw redirect({ to: '/' })
  }
}

function Shell() {
  const nav = useNavigate()
  useEffect(() => {
    const off = () => nav({ to: '/offline' })
    window.addEventListener('offline', off)
    return () => window.removeEventListener('offline', off)
  }, [nav])
  return (
    <div className="mx-auto min-h-dvh max-w-md text-[#f4e9ff]">
      <Outlet />
    </div>
  )
}

function Welcome() {
  const nav = useNavigate()
  const [phone, setPhone] = useState('+15551111111')
  const [code, setCode] = useState('')
  const [step, setStep] = useState<'phone' | 'code'>('phone')
  const [err, setErr] = useState('')
  const [hint, setHint] = useState('')

  const start = async () => {
    setErr('')
    const r = await api.post('/v1/auth/otp/start', { phone })
    setHint(r.devCode ? `Dev code: ${r.devCode}` : 'We sent a code.')
    setStep('code')
  }
  const verify = async () => {
    setErr('')
    try {
      const r = await api.post('/v1/auth/otp/verify', { phone, code })
      meCache = r.user
      nav({ to: r.user.onboardingStep === 'done' ? '/home' : '/onboarding' })
    } catch (e: any) {
      setErr(e.message)
    }
  }

  return (
    <div className="flex min-h-dvh flex-col justify-end px-6 pb-16 pt-24">
      <p className="text-sm tracking-[0.3em] text-fuchsia-300">GAMEMATCH</p>
      <h1 className="mt-4 text-4xl font-black leading-tight">
        Meet someone.
        <br />
        Play something.
      </h1>
      <p className="mt-3 text-fuchsia-100/70">See what happens.</p>
      {step === 'phone' ? (
        <div className="mt-10 space-y-3">
          <input className="w-full rounded-2xl bg-white/10 px-4 py-3 outline-none" value={phone} onChange={(e) => setPhone(e.target.value)} />
          <button className="w-full rounded-2xl bg-fuchsia-500 py-3 font-bold text-white shadow-[0_0_24px_#d946ef]" onClick={start}>
            Create account / log in
          </button>
          <button
            className="w-full rounded-2xl bg-white py-3 font-bold text-black"
            onClick={async () => {
              const r = await api.post('/v1/auth/oauth/apple')
              meCache = r.user
              nav({ to: r.user.onboardingStep === 'done' ? '/home' : '/onboarding' })
            }}
          >
            Continue with Apple
          </button>
          <button
            className="w-full rounded-2xl bg-white/10 py-3 font-bold"
            onClick={async () => {
              const r = await api.post('/v1/auth/oauth/google')
              meCache = r.user
              nav({ to: r.user.onboardingStep === 'done' ? '/home' : '/onboarding' })
            }}
          >
            Continue with Google
          </button>
          <p className="text-xs text-white/50">Demo: Alex +15551111111 · Jordan +15552222222 · code 123456. Apple/Google are local stand-ins until real keys are set.</p>
        </div>
      ) : (
        <div className="mt-10 space-y-3">
          <p className="text-sm text-fuchsia-200">{hint}</p>
          <input className="w-full rounded-2xl bg-white/10 px-4 py-3 tracking-[0.4em]" value={code} onChange={(e) => setCode(e.target.value)} placeholder="123456" />
          <button className="w-full rounded-2xl bg-fuchsia-500 py-3 font-bold" onClick={verify}>
            Continue
          </button>
          {err && <p className="text-rose-300">{err}</p>}
        </div>
      )}
    </div>
  )
}

function Onboarding() {
  const nav = useNavigate()
  const [cats, setCats] = useState<any>(null)
  const [step, setStep] = useState(0)
  const [intents, setIntents] = useState<string[]>(['dating'])
  const [traits, setTraits] = useState<string[]>([])
  const [name, setName] = useState('')
  const [dob, setDob] = useState('1998-01-15')
  const [metro, setMetro] = useState('')
  const [bio, setBio] = useState("I'm usually up for a game.")
  const [file, setFile] = useState<File | null>(null)
  const [gender, setGender] = useState('')
  const [err, setErr] = useState('')

  useEffect(() => {
    api.get('/v1/catalogs').then((c) => {
      setCats(c)
      setMetro(c.metros[0]?.id)
    })
  }, [])

  const toggle = (arr: string[], v: string, max: number) =>
    arr.includes(v) ? arr.filter((x) => x !== v) : arr.length >= max ? arr : [...arr, v]

  const finish = async () => {
    setErr('')
    if (!name.trim() || !file) {
      setErr('Name and a photo are required.')
      return
    }
    await api.patch('/v1/me', {
      intents,
      trait_ids: traits,
      name,
      dob,
      metro_id: metro,
      bio,
      gender_id: gender || undefined,
      age_attested: true,
      onboarding_step: 'done',
    })
    await api.upload('/v1/me/photos', file)
    nav({ to: '/home' })
  }

  if (!cats) return <p className="p-8">Loading…</p>
  const screens = [
    <div key="i" className="space-y-4">
      <h2 className="text-2xl font-black">What brings you here?</h2>
      {cats.intents.map((it: any) => (
        <button key={it.id} onClick={() => setIntents(toggle(intents, it.id, 4))} className={`block w-full rounded-2xl px-4 py-3 text-left ${intents.includes(it.id) ? 'bg-fuchsia-500' : 'bg-white/10'}`}>
          {it.label}
        </button>
      ))}
    </div>,
    <div key="t" className="space-y-3">
      <h2 className="text-2xl font-black">What&apos;s your vibe?</h2>
      <p className="text-sm text-white/60">Pick 5–8</p>
      <div className="flex flex-wrap gap-2">
        {cats.traits.map((t: any) => (
          <button key={t.id} onClick={() => setTraits(toggle(traits, t.id, 8))} className={`rounded-full px-3 py-2 ${traits.includes(t.id) ? 'bg-fuchsia-500' : 'bg-white/10'}`}>
            {t.emoji} {t.label}
          </button>
        ))}
      </div>
    </div>,
    <div key="p" className="space-y-3">
      <h2 className="text-2xl font-black">Your profile</h2>
      <input className="w-full rounded-2xl bg-white/10 px-4 py-3" placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} />
      <input className="w-full rounded-2xl bg-white/10 px-4 py-3" type="date" value={dob} onChange={(e) => setDob(e.target.value)} />
      <select className="w-full rounded-2xl bg-white/10 px-4 py-3" value={metro} onChange={(e) => setMetro(e.target.value)}>
        {cats.metros.map((m: any) => (
          <option key={m.id} value={m.id} className="text-black">
            {m.label}
          </option>
        ))}
      </select>
      <select className="w-full rounded-2xl bg-white/10 px-4 py-3" value={gender} onChange={(e) => setGender(e.target.value)}>
        <option value="" className="text-black">
          How you identify (optional)
        </option>
        {cats.genders.map((g: any) => (
          <option key={g.id} value={g.id} className="text-black">
            {g.label}
          </option>
        ))}
      </select>
      <textarea className="w-full rounded-2xl bg-white/10 px-4 py-3" value={bio} onChange={(e) => setBio(e.target.value)} />
      <p className="text-sm text-white/60">Photo required</p>
      <input type="file" accept="image/*" onChange={(e) => setFile(e.target.files?.[0] || null)} />
      {err && <p className="text-rose-300">{err}</p>}
    </div>,
  ]

  return (
    <div className="px-6 py-10">
      {screens[step]}
      <button
        className="mt-8 w-full rounded-2xl bg-fuchsia-500 py-3 font-bold"
        onClick={() => (step < 2 ? setStep(step + 1) : finish())}
      >
        {step < 2 ? 'Continue' : 'Save'}
      </button>
    </div>
  )
}

function Tabs({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col">
      <div className="flex-1 overflow-auto pb-24">{children}</div>
      <nav className="fixed bottom-0 left-1/2 z-10 flex w-full max-w-md -translate-x-1/2 justify-around border-t border-white/10 bg-[#0b0614]/95 py-3 text-xs">
        {[
          ['/home', 'Home'],
          ['/play', 'Play'],
          ['/match', 'Match'],
          ['/chat', 'Chat'],
          ['/me', 'Me'],
        ].map(([to, label]) => (
          <Link key={to} to={to} className="px-2 py-1 text-fuchsia-100/80">
            {label}
          </Link>
        ))}
      </nav>
    </div>
  )
}

function Home() {
  const nav = useNavigate()
  const [home, setHome] = useState<any>(null)
  useEffect(() => {
    api.get('/v1/home').then(setHome)
  }, [])
  const find = async () => {
    await api.post('/v1/queue', { gameKind: 'this_or_that' })
    nav({ to: '/queue' })
  }
  return (
    <Tabs>
      <div className="px-5 pt-8">
        <div className="flex items-center justify-between">
          <h1 className="text-xl font-black tracking-[0.2em]">GAMEMATCH</h1>
          <span className="text-fuchsia-300">●</span>
        </div>
        <div className="mt-8 rounded-3xl bg-gradient-to-br from-fuchsia-600 to-violet-800 p-6 shadow-[0_0_40px_#a21caf66]">
          <p className="text-sm uppercase tracking-widest text-white/80">Live now</p>
          <h2 className="mt-2 text-2xl font-black">Find someone</h2>
          <p className="mt-1 text-white/70">{home?.queueDepth ?? 0} waiting to play</p>
          <button className="mt-5 w-full rounded-2xl bg-white py-3 font-bold text-fuchsia-700" onClick={find}>
            Join game
          </button>
        </div>
        {home?.invites?.length > 0 && (
          <div className="mt-6 space-y-2">
            <h3 className="text-sm uppercase tracking-widest text-white/50">Invites</h3>
            {home.invites.map((i: any) => (
              <button
                key={i.id}
                className="w-full rounded-2xl bg-white/10 p-4 text-left"
                onClick={async () => {
                  const r = await api.post(`/v1/invites/${i.id}/accept`)
                  nav({ to: '/session/$id', params: { id: r.sessionId } })
                }}
              >
                {i.other?.name} · {i.gameKind.replaceAll('_', ' ')}
                <span
                  className="ml-3 text-xs text-white/40"
                  onClick={async (e) => {
                    e.stopPropagation()
                    await api.post(`/v1/invites/${i.id}/decline`)
                    setHome(await api.get('/v1/home'))
                  }}
                >
                  decline
                </span>
              </button>
            ))}
          </div>
        )}
      </div>
    </Tabs>
  )
}

function Play() {
  const nav = useNavigate()
  const start = async (kind: string) => {
    await api.post('/v1/queue', { gameKind: kind })
    nav({ to: '/queue' })
  }
  return (
    <Tabs>
      <div className="px-5 pt-8 space-y-3">
        <h1 className="text-2xl font-black">Play</h1>
        {[
          ['this_or_that', 'This or That'],
          ['twenty_questions', '20 Questions'],
          ['guess_my_answer', 'Guess My Answer'],
        ].map(([k, l]) => (
          <button key={k} className="w-full rounded-2xl bg-white/10 p-5 text-left text-lg font-bold" onClick={() => start(k)}>
            {l}
          </button>
        ))}
      </div>
    </Tabs>
  )
}

function Queue() {
  const nav = useNavigate()
  const [st, setSt] = useState<any>(null)
  useEffect(() => {
    let stop = false
    const tick = async () => {
      const s = await api.get('/v1/queue/status')
      if (stop) return
      setSt(s)
      if (s.state === 'matched') {
        stop = true
        await api.post(`/v1/sessions/${s.sessionId}/join`)
        nav({ to: '/session/$id', params: { id: s.sessionId } })
      }
    }
    tick()
    const t = setInterval(tick, 2000)
    return () => {
      stop = true
      clearInterval(t)
    }
  }, [nav])
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center px-6 text-center">
      <div className="h-16 w-16 animate-pulse rounded-full bg-fuchsia-500 shadow-[0_0_40px_#d946ef]" />
      <h1 className="mt-6 text-2xl font-black">You&apos;re in the queue</h1>
      <p className="mt-2 text-white/60">Waited {st?.waitedSec ?? 0}s</p>
      {st?.canPractice && (
        <button
          className="mt-6 rounded-2xl bg-white/10 px-6 py-3"
          onClick={async () => {
            await api.post('/v1/queue', { gameKind: st?.gameKind || 'this_or_that', allowPractice: true })
          }}
        >
          Practice vs House
        </button>
      )}
      <button className="mt-4 text-sm text-white/40" onClick={async () => { await api.del('/v1/queue'); nav({ to: '/home' }) }}>
        Leave
      </button>
    </div>
  )
}

function SessionPage({ id }: { id: string }) {
  const nav = useNavigate()
  const [s, setS] = useState<any>(null)
  useEffect(() => {
    let live = true
    const poll = async () => {
      const v = await api.get(`/v1/sessions/${id}`)
      if (live) setS(v)
    }
    poll()
    const t = setInterval(poll, 1000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [id])

  if (!s) return <p className="p-8">Connecting…</p>
  if (s.state === 'pending' || s.state === 'countdown') {
    return (
      <div className="flex min-h-dvh items-center justify-center text-center">
        <div>
          <p className="tracking-[0.3em] text-fuchsia-300">GET READY</p>
          <h1 className="mt-4 text-3xl font-black">vs {s.opponent?.name ?? '…'}</h1>
        </div>
      </div>
    )
  }
  if (s.state === 'completed' && s.completed) {
    const c = s.completed
    return (
      <div className="flex min-h-dvh flex-col items-center justify-center px-6 text-center">
        <p className="tracking-[0.3em] text-fuchsia-300">GAME OVER</p>
        <h1 className="mt-3 text-3xl font-black">You + {s.opponent?.name}</h1>
        {c.snapshot?.percent != null && <p className="mt-4 text-5xl font-black text-fuchsia-400">{c.snapshot.percent}%</p>}
        <ul className="mt-4 space-y-1 text-white/70">
          {(c.snapshot?.reasons || []).map((r: string) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
        {c.connectEligible && c.pairId && (
          <div className="mt-8 flex w-full gap-3">
            <button
              className="flex-1 rounded-2xl bg-fuchsia-500 py-3 font-bold"
              onClick={async () => {
                await api.post(`/v1/pairs/${c.pairId}/connect`, { action: 'connect' })
                nav({ to: '/match' })
              }}
            >
              Connect
            </button>
            <button
              className="flex-1 rounded-2xl bg-white/10 py-3"
              onClick={async () => {
                await api.post(`/v1/pairs/${c.pairId}/connect`, { action: 'pass' })
                nav({ to: '/home' })
              }}
            >
              Pass
            </button>
          </div>
        )}
        {c.practiceOpponent && (
          <button className="mt-6 rounded-2xl bg-white/10 px-6 py-3" onClick={() => nav({ to: '/play' })}>
            Back to Play
          </button>
        )}
        {c.connectEligible && s.opponent?.id && s.opponent.id !== 'house' && (
          <button
            className="mt-4 text-sm text-fuchsia-200"
            onClick={async () => {
              await api.post(`/v1/sessions/${id}/rematch`)
              nav({ to: '/home' })
            }}
          >
            Challenge them to play again
          </button>
        )}
      </div>
    )
  }
  if (s.state === 'forfeit' || s.state === 'cancelled') {
    return (
      <div className="flex min-h-dvh flex-col items-center justify-center px-6">
        <h1 className="text-2xl font-black">Game ended</h1>
        <button className="mt-4 rounded-2xl bg-fuchsia-500 px-6 py-3" onClick={() => nav({ to: '/play' })}>
          Play
        </button>
      </div>
    )
  }

  const p = s.round?.prompt
  const choose = async (payload: any) => {
    await api.post(`/v1/sessions/${id}/answer`, { payload })
  }

  return (
    <div className="flex min-h-dvh flex-col px-5 py-10">
      <p className="text-center text-sm text-white/50">
        Round {s.round?.index}/{s.round?.total} · {s.opponent?.name}
      </p>
      {s.reveal ? (
        <div className="mt-10 text-center">
          <p className="text-lg">You: {JSON.stringify(s.reveal.you.payload)}</p>
          <p className="mt-2 text-lg">Them: {JSON.stringify(s.reveal.opponent.payload)}</p>
          {s.reveal.same && <p className="mt-6 text-2xl font-black text-fuchsia-400">Same energy</p>}
        </div>
      ) : p?.left ? (
        <div className="mt-8 space-y-4">
          <h2 className="text-center text-xl font-black">This or That</h2>
          <button className="w-full rounded-3xl bg-violet-600 py-8 text-2xl font-black" onClick={() => choose({ choice: 'left' })} disabled={s.round.youSubmitted}>
            {p.left.label}
          </button>
          <p className="text-center text-white/40">vs</p>
          <button className="w-full rounded-3xl bg-fuchsia-600 py-8 text-2xl font-black" onClick={() => choose({ choice: 'right' })} disabled={s.round.youSubmitted}>
            {p.right.label}
          </button>
        </div>
      ) : p?.options ? (
        <div className="mt-8 space-y-3">
          <h2 className="text-center text-xl font-black">{p.question}</h2>
          {s.round.yourRole && <p className="text-center text-sm text-fuchsia-200">{s.round.yourRole === 'answerer' ? 'Your answer' : 'Guess theirs'}</p>}
          {p.options.map((o: any) => (
            <button key={o.id} className="w-full rounded-2xl bg-white/10 py-4" onClick={() => choose({ optionId: o.id })} disabled={s.round.youSubmitted}>
              {o.label}
            </button>
          ))}
        </div>
      ) : (
        <p className="mt-10 text-center">Get ready…</p>
      )}
      {s.round?.youSubmitted && !s.reveal && <p className="mt-6 text-center text-white/50">Waiting on them…</p>}
      <button className="mt-auto pt-8 text-sm text-white/40" onClick={async () => { await api.post(`/v1/sessions/${id}/leave`); nav({ to: '/play' }) }}>
        Leave game
      </button>
    </div>
  )
}

function MatchTab() {
  const [rows, setRows] = useState<any[]>([])
  const reload = () => api.get('/v1/pairs').then((r) => setRows(r.items))
  useEffect(() => {
    reload()
  }, [])
  return (
    <Tabs>
      <div className="px-5 pt-8 space-y-3">
        <h1 className="text-2xl font-black">Match</h1>
        {rows.length === 0 && <p className="text-white/50">Play someone first.</p>}
        {rows.map((p) => (
          <div key={p.id} className="rounded-2xl bg-white/10 p-4">
            <div className="flex gap-3">
              {p.other.photoUrl && <img src={p.other.photoUrl} alt="" className="h-14 w-14 rounded-full object-cover" />}
              <div>
                <p className="font-bold">{p.other.name}</p>
                <p className="text-sm text-white/50">{p.state.replaceAll('_', ' ')}</p>
              </div>
            </div>
            <div className="mt-3 flex flex-wrap gap-2 text-sm">
              {p.state === 'open_play' && (
                <button className="rounded-xl bg-fuchsia-500 px-3 py-1" onClick={async () => { await api.post(`/v1/pairs/${p.id}/connect`, { action: 'connect' }); reload() }}>
                  Connect
                </button>
              )}
              {p.inviteEligible && (
                <button className="rounded-xl bg-white/10 px-3 py-1" onClick={async () => { await api.post('/v1/invites', { targetUserId: p.other.id, gameKind: 'this_or_that' }); reload() }}>
                  Challenge
                </button>
              )}
              <button className="rounded-xl bg-white/10 px-3 py-1" onClick={async () => { await api.post('/v1/reports', { userId: p.other.id, reason: 'other' }); }}>
                Report
              </button>
              <button className="rounded-xl bg-white/10 px-3 py-1" onClick={async () => { await api.post('/v1/blocks', { userId: p.other.id }); reload() }}>
                Block
              </button>
              {p.state === 'mutual' && (
                <button className="rounded-xl bg-white/10 px-3 py-1" onClick={async () => { await api.post(`/v1/pairs/${p.id}/unmatch`); reload() }}>
                  Unmatch
                </button>
              )}
            </div>
          </div>
        ))}
      </div>
    </Tabs>
  )
}

function ChatList() {
  const nav = useNavigate()
  const [rows, setRows] = useState<any[]>([])
  useEffect(() => {
    api.get('/v1/threads').then((r) => setRows(r.items))
  }, [])
  return (
    <Tabs>
      <div className="px-5 pt-8 space-y-3">
        <h1 className="text-2xl font-black">Chat</h1>
        {rows.length === 0 && <p className="text-white/50">Connect with someone to start a chat.</p>}
        {rows.map((t) => (
          <button key={t.threadId} className="w-full rounded-2xl bg-white/10 p-4 text-left" onClick={() => nav({ to: '/chat/$id', params: { id: t.threadId } })}>
            <p className="font-bold">{t.other?.name}</p>
            <p className="truncate text-sm text-white/50">{t.last?.body}</p>
          </button>
        ))}
      </div>
    </Tabs>
  )
}

function ChatThread({ id }: { id: string }) {
  const [data, setData] = useState<any>(null)
  const [text, setText] = useState('')
  const [threadMeta, setThreadMeta] = useState<any>(null)
  const load = () => api.get(`/v1/threads/${id}/messages`).then(setData)
  useEffect(() => {
    load()
    api.get('/v1/threads').then((r) => setThreadMeta(r.items.find((x: any) => x.threadId === id)))
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [id])
  const send = async (body: string) => {
    await api.post(`/v1/threads/${id}/messages`, { body })
    setText('')
    load()
  }
  return (
    <div className="flex min-h-dvh flex-col px-4 pb-8 pt-6">
      <div className="flex items-center justify-between">
        <Link to="/chat" className="text-sm text-fuchsia-300">
          ← Chat
        </Link>
        {threadMeta?.other?.id && (
          <button
            className="text-xs text-fuchsia-200"
            onClick={() => api.post('/v1/invites', { targetUserId: threadMeta.other.id, gameKind: 'this_or_that' })}
          >
            Challenge
          </button>
        )}
      </div>
      <div className="mt-4 flex-1 space-y-2 overflow-auto">
        {data?.items?.map((m: any) => (
          <div key={m.id} className={`max-w-[80%] rounded-2xl px-3 py-2 ${m.kind !== 'text' ? 'bg-fuchsia-500/20' : 'bg-white/10'}`}>
            <p>{m.body}</p>
            {m.meta?.actions && (
              <div className="mt-2 flex flex-wrap gap-2">
                {m.meta.actions.map((a: string) => {
                  const map: any = { me: "I'll pick", them: 'You pick', compete: "Let's compete", play_again: 'Play again' }
                  return (
                    <button key={a} className="rounded-full bg-fuchsia-500 px-3 py-1 text-xs" onClick={() => send(map[a])}>
                      {map[a]}
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        ))}
      </div>
      {data?.state === 'open' && (
        <form
          className="mt-3 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            send(text)
          }}
        >
          <input className="flex-1 rounded-2xl bg-white/10 px-3 py-2" value={text} onChange={(e) => setText(e.target.value)} />
          <button className="rounded-2xl bg-fuchsia-500 px-4">Send</button>
        </form>
      )}
    </div>
  )
}

function MePage() {
  const nav = useNavigate()
  const { installed } = useInstallPrompt()
  const [u, setU] = useState<Me | null>(null)
  const [legal, setLegal] = useState<any>(null)
  const [xp, setXp] = useState<any>(null)
  useEffect(() => {
    api.get('/v1/me').then((r) => setU(r.user))
    api.get('/v1/legal').then(setLegal)
    api.get('/v1/me/xp').then(setXp)
  }, [])
  return (
    <Tabs>
      <div className="px-5 pt-8 space-y-4">
        <h1 className="text-2xl font-black">{u?.name ?? 'You'}</h1>
        <p className="text-white/60">
          Level {u?.level} · {u?.xp} XP
        </p>
        <div className="flex flex-wrap gap-2">
          {(xp?.badges || []).map((b: any) => (
            <span key={b.id} className="rounded-full bg-fuchsia-500/30 px-3 py-1 text-sm">
              {b.label}
            </span>
          ))}
        </div>
        <label className="flex items-center gap-3 rounded-2xl bg-white/10 p-4">
          <input
            type="checkbox"
            checked={!!u?.incognito}
            onChange={async (e) => {
              await api.patch('/v1/me', { incognito: e.target.checked })
              const r = await api.get('/v1/me')
              setU(r.user)
            }}
          />
          Incognito (hide from lobby)
        </label>
        {!installed && (
          <Link to="/install" className="block rounded-2xl bg-white/5 p-4 text-sm text-white/70">
            Install app
          </Link>
        )}
        <Link to="/legal" className="block rounded-2xl bg-white/5 p-4 text-sm text-white/70">
          Terms & privacy
        </Link>
        {u?.role === 'admin' && (
          <Link to="/staff" className="block rounded-2xl bg-fuchsia-500/20 p-4 text-sm">
            Staff tools
          </Link>
        )}
        <p className="hidden">{legal?.privacy}</p>
        <button className="w-full rounded-2xl bg-white/10 py-3" onClick={async () => { await api.post('/v1/auth/logout'); nav({ to: '/' }) }}>
          Log out
        </button>
        <button className="w-full text-sm text-rose-300" onClick={async () => { await api.del('/v1/me'); nav({ to: '/' }) }}>
          Delete account
        </button>
      </div>
    </Tabs>
  )
}

function InstallPage() {
  const { canInstall, installed, platform, install } = useInstallPrompt()
  const [status, setStatus] = useState<'idle' | 'accepted' | 'dismissed'>('idle')

  const doInstall = async () => {
    const outcome = await install()
    if (outcome === 'accepted' || outcome === 'dismissed') setStatus(outcome)
  }

  return (
    <div className="px-5 py-10 space-y-4">
      <Link to="/me" className="text-sm text-fuchsia-300">
        ← Me
      </Link>
      <h1 className="text-2xl font-black">Install GameMatch</h1>
      <p className="text-white/70">Add GameMatch to your home screen for a full-screen, app-like experience.</p>
      {installed || status === 'accepted' ? (
        <div className="rounded-2xl bg-fuchsia-500/20 p-4" role="status">
          <p className="font-bold">You&apos;re all set</p>
          <p className="mt-1 text-sm text-white/70">GameMatch is installed. Open it from your home screen.</p>
        </div>
      ) : canInstall ? (
        <div className="space-y-3">
          <button className="w-full rounded-2xl bg-fuchsia-500 py-3 font-bold text-white shadow-[0_0_24px_#d946ef]" onClick={doInstall}>
            Install GameMatch
          </button>
          {status === 'dismissed' && <p className="text-sm text-white/60">No problem. You can add it later from your browser menu.</p>}
        </div>
      ) : platform === 'ios' ? (
        <div className="space-y-3 rounded-2xl bg-white/10 p-4">
          <p className="font-bold">Add to Home Screen</p>
          <ol className="list-decimal space-y-2 pl-5 text-sm text-white/70">
            <li>Tap the Share button in Safari.</li>
            <li>Scroll down and choose “Add to Home Screen”.</li>
            <li>Tap Add, then GameMatch opens full screen.</li>
          </ol>
          <p className="text-xs text-white/50">Use Safari for this. Other iOS browsers can&apos;t install apps.</p>
        </div>
      ) : (
        <div className="rounded-2xl bg-white/10 p-4 text-sm text-white/70">
          <p className="font-bold text-white">Install from your browser menu</p>
          <p className="mt-2">Open the browser menu and choose “Install app” or “Add to Home Screen”.</p>
        </div>
      )}
      <p className="text-xs text-white/50">Installing keeps you signed in and enables game notifications.</p>
    </div>
  )
}

function LegalPage() {
  const [legal, setLegal] = useState<any>(null)
  useEffect(() => {
    api.get('/v1/legal').then(setLegal)
  }, [])
  return (
    <div className="px-5 py-10 space-y-4">
      <Link to="/me" className="text-sm text-fuchsia-300">
        ← Me
      </Link>
      <h1 className="text-2xl font-black">Terms</h1>
      <p className="text-white/70">{legal?.terms}</p>
      <h2 className="text-xl font-black">Privacy</h2>
      <p className="text-white/70">{legal?.privacy}</p>
    </div>
  )
}

function StaffPage() {
  const [reports, setReports] = useState<any[]>([])
  const [allow, setAllow] = useState<any>(null)
  const [phone, setPhone] = useState('')
  useEffect(() => {
    api.get('/v1/internal/mod/reports').then((r) => setReports(r.items || []))
    api.get('/v1/staff/allowlist').then(setAllow)
  }, [])
  return (
    <div className="px-5 py-10 space-y-4">
      <Link to="/me" className="text-sm text-fuchsia-300">
        ← Me
      </Link>
      <h1 className="text-2xl font-black">Staff</h1>
      <p className="text-sm text-white/60">Public signup: {allow?.publicSignup ? 'on' : 'off'} · {allow?.count} invited phones</p>
      <button className="rounded-xl bg-white/10 px-3 py-2 text-sm" onClick={async () => setAllow(await api.post('/v1/staff/allowlist', { public_signup: !allow?.publicSignup }))}>
        Toggle public signup
      </button>
      <div className="flex gap-2">
        <input className="flex-1 rounded-xl bg-white/10 px-3 py-2" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="+1…" />
        <button className="rounded-xl bg-fuchsia-500 px-3" onClick={async () => { setAllow(await api.post('/v1/staff/allowlist', { phone })); setPhone('') }}>
          Invite
        </button>
      </div>
      <h2 className="font-bold">Reports</h2>
      {reports.map((r) => (
        <div key={r.id} className="rounded-xl bg-white/10 p-3 text-sm">
          {r.reason} · {r.status}
        </div>
      ))}
    </div>
  )
}

function OfflinePage() {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center px-6 text-center">
      <h1 className="text-2xl font-black">You&apos;re offline</h1>
      <p className="mt-2 text-white/60">Games need a connection. Rejoin when you&apos;re back.</p>
    </div>
  )
}

function Boot() {
  const nav = useNavigate()
  useEffect(() => {
    api
      .get('/v1/auth/session')
      .then((r) => nav({ to: r.user.onboardingStep === 'done' ? '/home' : '/onboarding' }))
      .catch(() => nav({ to: '/' }))
  }, [nav])
  return <p className="p-10">Loading…</p>
}

const rootRoute = createRootRoute({ component: Shell })
const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: Welcome })
const bootRoute = createRoute({ getParentRoute: () => rootRoute, path: '/boot', component: Boot })
const onRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/onboarding',
  beforeLoad: requireUser,
  component: Onboarding,
})
const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/home', beforeLoad: requireUser, component: Home })
const playRoute = createRoute({ getParentRoute: () => rootRoute, path: '/play', beforeLoad: requireUser, component: Play })
const queueRoute = createRoute({ getParentRoute: () => rootRoute, path: '/queue', beforeLoad: requireUser, component: Queue })
const sessionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/session/$id',
  beforeLoad: requireUser,
  component: function S() {
    const { id } = sessionRoute.useParams()
    return <SessionPage id={id} />
  },
})
const matchRoute = createRoute({ getParentRoute: () => rootRoute, path: '/match', beforeLoad: requireUser, component: MatchTab })
const chatRoute = createRoute({ getParentRoute: () => rootRoute, path: '/chat', beforeLoad: requireUser, component: ChatList })
const threadRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/chat/$id',
  beforeLoad: requireUser,
  component: function T() {
    const { id } = threadRoute.useParams()
    return <ChatThread id={id} />
  },
})
const meRoute = createRoute({ getParentRoute: () => rootRoute, path: '/me', beforeLoad: requireUser, component: MePage })
const installRoute = createRoute({ getParentRoute: () => rootRoute, path: '/install', beforeLoad: requireUser, component: InstallPage })
const legalRoute = createRoute({ getParentRoute: () => rootRoute, path: '/legal', component: LegalPage })
const staffRoute = createRoute({ getParentRoute: () => rootRoute, path: '/staff', beforeLoad: requireUser, component: StaffPage })
const offlineRoute = createRoute({ getParentRoute: () => rootRoute, path: '/offline', component: OfflinePage })

const routeTree = rootRoute.addChildren([
  indexRoute,
  bootRoute,
  onRoute,
  homeRoute,
  playRoute,
  queueRoute,
  sessionRoute,
  matchRoute,
  chatRoute,
  threadRoute,
  meRoute,
  installRoute,
  legalRoute,
  staffRoute,
  offlineRoute,
])
const router = createRouter({ routeTree })

export default function App() {
  return <RouterProvider router={router} />
}
