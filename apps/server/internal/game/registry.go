package game

import (
	"encoding/json"
	"time"
)

// Protocol is the round shape a game uses. The client renders one component
// per protocol (see apps/web/src/protocols.ts).
type Protocol string

const (
	ProtocolPick2    Protocol = "pick2"    // two options
	ProtocolPick4    Protocol = "pick4"    // four options
	ProtocolPhased   Protocol = "phased"   // answer, then predict
	ProtocolSpectrum Protocol = "spectrum" // five stops on a line
	ProtocolOrder    Protocol = "order"    // rank three items
	ProtocolStance   Protocol = "stance"   // agree / disagree / it depends
	ProtocolCoop     Protocol = "coop"     // match on purpose
	ProtocolRapid    Protocol = "rapid"    // short timer, many rounds
	ProtocolRate     Protocol = "rate"     // rate one to five
	ProtocolBranch   Protocol = "branch"   // linked choices
)

// Game declares a playable GameMatch game. A new game is one entry here, its
// prompts in a content migration, and a client renderer for its protocol.
type Game struct {
	Kind        string
	Label       string
	Protocol    Protocol
	Rounds      int
	Timer       time.Duration
	GuessWindow time.Duration // phased only
	HouseAnswer func(payload []byte) json.RawMessage
	TagPrefixes []string // tag prefixes this game contributes to the scorer sets
	Chips       []string // plain-language reasons this game can unlock
	Ready       bool
}

// Registry lists every game the app can play.
var Registry = map[string]Game{}

// RegistryOrder is the display order (Play screen lists games in this order).
var RegistryOrder = []string{}

func add(entry Game) {
	Registry[entry.Kind] = entry
	RegistryOrder = append(RegistryOrder, entry.Kind)
}

func init() {
	add(Game{
		Kind: "this_or_that", Label: "This or That", Protocol: ProtocolPick2,
		Rounds: 8, Timer: 8 * time.Second,
		HouseAnswer: func([]byte) json.RawMessage { return json.RawMessage(`{"choice":"left"}`) },
		TagPrefixes: []string{"interest.", "lifestyle."},
		Chips:       []string{"Same taste", "Similar lifestyle"},
		Ready:       true,
	})
	add(Game{
		Kind: "twenty_questions", Label: "20 Questions", Protocol: ProtocolPick4,
		Rounds: 6, Timer: 20 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: []string{"interest."},
		Chips:       []string{"Similar interests"},
		Ready:       true,
	})
	add(Game{
		Kind: "guess_my_answer", Label: "Guess My Answer", Protocol: ProtocolPhased,
		Rounds: 6, Timer: 8 * time.Second, GuessWindow: 12 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: nil, // guesses are not preferences
		Chips:       []string{"Reads the room"},
		Ready:       true,
	})
	add(Game{
		Kind: "where_do_you_land", Label: "Where Do You Land", Protocol: ProtocolSpectrum,
		Rounds: 8, Timer: 10 * time.Second,
		HouseAnswer: func([]byte) json.RawMessage { return json.RawMessage(`{"stop":2}`) },
		TagPrefixes: []string{"values."},
		Chips:       []string{"Closer than you'd think"},
		Ready:       true,
	})
	add(Game{
		Kind: "rank_your_top_3", Label: "Rank Your Top 3", Protocol: ProtocolOrder,
		Rounds: 5, Timer: 12 * time.Second,
		HouseAnswer: orderAnswer,
		TagPrefixes: []string{"priorities."},
		Chips:       []string{"Same number one"},
		Ready:       true,
	})
	add(Game{
		Kind: "hot_take", Label: "Hot Take", Protocol: ProtocolStance,
		Rounds: 6, Timer: 10 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: []string{"values."},
		Chips:       []string{"Agree where it counts"},
		Ready:       true,
	})
	add(Game{
		Kind: "same_page", Label: "Same Page", Protocol: ProtocolCoop,
		Rounds: 6, Timer: 8 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: []string{"mindset."},
		Chips:       []string{"On the same page"},
		Ready:       true,
	})
	add(Game{
		Kind: "speed_round", Label: "Speed Round", Protocol: ProtocolRapid,
		Rounds: 12, Timer: 5 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: []string{"energy."},
		Chips:       []string{"Same speed"},
		Ready:       true,
	})
	add(Game{
		Kind: "one_free_evening", Label: "One Free Evening", Protocol: ProtocolPick4,
		Rounds: 5, Timer: 12 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: []string{"priorities."},
		Chips:       []string{"Protect the same things"},
		Ready:       true,
	})
	add(Game{
		Kind: "odd_one_out", Label: "Odd One Out", Protocol: ProtocolPick4,
		Rounds: 6, Timer: 10 * time.Second,
		HouseAnswer: firstOptionAnswer,
		TagPrefixes: []string{"mindset."},
		Chips:       []string{"Sort things the same way"},
		Ready:       true,
	})
	add(Game{
		Kind: "rate_the_night", Label: "Rate the Night", Protocol: ProtocolRate,
		Rounds: 5, Timer: 10 * time.Second,
		HouseAnswer: func([]byte) json.RawMessage { return json.RawMessage(`{"rating":3}`) },
		TagPrefixes: []string{"energy."},
		Chips:       []string{"Same idea of a good night"},
		Ready:       true,
	})
	add(Game{
		Kind: "either_way", Label: "Either Way", Protocol: ProtocolBranch,
		Rounds: 3, Timer: 15 * time.Second,
		HouseAnswer: func([]byte) json.RawMessage { return json.RawMessage(`{"path":["left","left","left"]}`) },
		TagPrefixes: []string{"mindset."},
		Chips:       []string{"Think in the same steps"},
		Ready:       true,
	})
}

// For returns a copy of a game's declaration, or a zero entry.
func For(kind string) Game { return Registry[kind] }

// RoundsFor reports how many rounds a game has (default 8).
func RoundsFor(kind string) int {
	if entry, ok := Registry[kind]; ok && entry.Rounds > 0 {
		return entry.Rounds
	}
	return 8
}

// TimerFor reports a round's answer deadline (default 8s).
func TimerFor(kind string) time.Duration {
	if entry, ok := Registry[kind]; ok && entry.Timer > 0 {
		return entry.Timer
	}
	return 8 * time.Second
}

// GuessWindowFor reports the phased guess deadline (default 12s).
func GuessWindowFor(kind string) time.Duration {
	if entry, ok := Registry[kind]; ok && entry.GuessWindow > 0 {
		return entry.GuessWindow
	}
	return 12 * time.Second
}

// HouseAnswerFor returns the practice opponent's answer for a prompt.
func HouseAnswerFor(kind string, payload []byte) json.RawMessage {
	if entry, ok := Registry[kind]; ok && entry.HouseAnswer != nil {
		return entry.HouseAnswer(payload)
	}
	return firstOptionAnswer(payload)
}

// firstOptionAnswer picks the first reachable option in a prompt payload.
// Prompts may carry options, items, stops, or a left/right pair.
func firstOptionAnswer(payload []byte) json.RawMessage {
	var prompt struct {
		Left *struct {
			ID string `json:"id"`
		} `json:"left"`
		Options []struct {
			ID string `json:"id"`
		} `json:"options"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Stops []struct {
			ID string `json:"id"`
		} `json:"stops"`
	}
	if err := json.Unmarshal(payload, &prompt); err != nil {
		return json.RawMessage(`{}`)
	}
	if len(prompt.Options) > 0 {
		b, _ := json.Marshal(map[string]string{"optionId": prompt.Options[0].ID})
		return b
	}
	if len(prompt.Items) > 0 {
		b, _ := json.Marshal(map[string]string{"optionId": prompt.Items[0].ID})
		return b
	}
	if len(prompt.Stops) > 0 {
		b, _ := json.Marshal(map[string]string{"optionId": prompt.Stops[0].ID})
		return b
	}
	if prompt.Left != nil {
		b, _ := json.Marshal(map[string]string{"optionId": prompt.Left.ID})
		return b
	}
	return json.RawMessage(`{"optionId":"a"}`)
}

// orderAnswer returns the items in their given order.
func orderAnswer(payload []byte) json.RawMessage {
	var prompt struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(payload, &prompt); err != nil || len(prompt.Items) == 0 {
		return json.RawMessage(`{"order":[]}`)
	}
	order := make([]string, 0, len(prompt.Items))
	for _, item := range prompt.Items {
		order = append(order, item.ID)
	}
	b, _ := json.Marshal(map[string]any{"order": order})
	return b
}
