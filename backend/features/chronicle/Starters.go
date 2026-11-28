package chronicle

import "github.com/SgtFlame/foundation/backend/features/records"

// Starter is a set of entry types a writer mode offers (forge 35f-6),
// which a project copies and then edits as its own: the fantasy story
// bible, and the sci-fi one with technology and physical rules.
type Starter struct {
	ID          string
	Title       string
	Description string
	Types       []EntryType
}

func text(name, description string) records.Field {
	return records.Field{Name: name, Type: "text", Description: description}
}

func longText(name, description string) records.Field {
	return records.Field{Name: name, Type: "longText", Description: description}
}

func entryRef(name, description string, many bool) records.Field {
	return records.Field{Name: name, Type: "relation", Description: description, Relation: &records.Relation{Entity: EntryRelation, Many: many}}
}

func choice(name, description string, options ...string) records.Field {
	f := records.Field{Name: name, Type: "enum", Description: description}
	for _, o := range options {
		f.Options = append(f.Options, records.Option{ID: fieldID(o), Name: o})
	}
	return f
}

var fantasyTypes = []EntryType{
	{ID: "character", Title: "Characters", Icon: "🧝", Description: "a person or creature of the story, with who they are, where they live and what they want",
		RecordFields: []records.Field{text("Age", "How old they are, as the story stands"), choice("Status", "Whether they are alive", "Alive", "Dead", "Missing", "Unknown"),
			entryRef("Home", "The Place they live", false), entryRef("Factions", "The Factions they belong to", true),
			text("Appearance", "What they look like"), longText("Voice", "How they speak: vocabulary, rhythm, tics"), text("Goal", "What they want")}},
	{ID: "place", Title: "Places", Icon: "🏰", Description: "a place of the story: a city, a region, a building, a world",
		RecordFields: []records.Field{choice("Kind", "What sort of place", "World", "Region", "City", "Building", "Wilderness", "Other"),
			entryRef("Located in", "The Place it lies within", false), text("Climate", "Its weather and seasons")}},
	{ID: "faction", Title: "Factions", Icon: "🛡️", Description: "a group of the story: a house, a guild, an order, a nation",
		RecordFields: []records.Field{entryRef("Leader", "The Character who leads it", false), entryRef("Seat", "The Place it is based", false), text("Aims", "What it wants")}},
	{ID: "object", Title: "Objects", Icon: "🗡️", Description: "an object that matters to the story: an artefact, a weapon, a book",
		RecordFields: []records.Field{entryRef("Owner", "The Character who has it", false), entryRef("Location", "The Place it is", false), longText("Powers", "What it can do")}},
	{ID: "magicRule", Title: "Magic rules", Icon: "✨", Description: "a rule of the story's magic: what it allows, what it costs and what it never does",
		RecordFields: []records.Field{longText("Allows", "What it makes possible"), longText("Cost", "What using it costs"), longText("Limits", "What it can never do")}},
	{ID: "storyEvent", Title: "Story events", Icon: "📜", Description: "something that happens in the story or its history, with when and where",
		RecordFields: []records.Field{text("When", "When it happens, in the story's calendar"), entryRef("Where", "The Place it happens", false),
			entryRef("Who", "The Characters involved", true), text("Chapter", "The chapter it is told in")}},
}

// sciFiTypes are the fantasy set with technologies and physical rules
// in place of magic rules.
var sciFiTypes = append(append([]EntryType{}, fantasyTypes[:4]...),
	EntryType{ID: "technology", Title: "Technologies", Icon: "🛰️", Description: "a technology of the story: what it does, how it works and what it cannot do",
		RecordFields: []records.Field{longText("Does", "What it does"), longText("Works by", "How it works, as far as the story says"),
			longText("Limits", "What it cannot do"), text("Since", "When it was invented")}},
	EntryType{ID: "physicalRule", Title: "Physical rules", Icon: "⚛️", Description: "a law of the story's physics, where it follows or departs from real science",
		RecordFields: []records.Field{longText("Rule", "The rule itself"), choice("Basis", "How it relates to real science", "Real", "Extrapolated", "Invented"),
			longText("Consequences", "What follows from it")}},
	fantasyTypes[5],
)

// Starters are the starter sets, in the order a picker lists them.
var Starters = []Starter{
	{ID: "fantasy", Title: "Fantasy story bible", Description: "Characters, places, factions, objects, magic rules and story events", Types: fantasyTypes},
	{ID: "scifi", Title: "Sci-fi story bible", Description: "The fantasy set with technologies and physical rules in place of magic", Types: sciFiTypes},
}

// StarterOf is the starter set with id.
func StarterOf(id string) (Starter, bool) {
	for _, s := range Starters {
		if s.ID == id {
			return s, true
		}
	}
	return Starter{}, false
}
