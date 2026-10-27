package highlight

func init() { RegisterLanguage("mizar", Mizar()) }

// Mizar returns the Mizar language definition (a port of languages/mizar.js).
func Mizar() *Language {
	return &Language{
		Name: "Mizar",
		Keywords: Keywords{
			"keyword": "environ vocabularies notations constructors definitions " +
				"registrations theorems schemes requirements begin end definition " +
				"registration cluster existence pred func defpred deffunc theorem " +
				"proof let take assume then thus hence ex for st holds consider " +
				"reconsider such that and in provided of as from be being by means " +
				"equals implies iff redefine define now not or attr is mode " +
				"suppose per cases set thesis contradiction scheme reserve struct " +
				"correctness compatibility coherence symmetry assymetry " +
				"reflexivity irreflexivity connectedness uniqueness commutativity " +
				"idempotence involutiveness projectivity",
		},
		Contains: []*Mode{
			CommentMode(`::`, `$`),
		},
	}
}
