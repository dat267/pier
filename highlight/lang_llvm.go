package highlight

func init() { RegisterLanguage("llvm", LLVM()) }

// LLVM returns the LLVM IR language definition (a port of languages/llvm.js).
func LLVM() *Language {
	const identRe = `([-a-zA-Z$._][\w$.-]*)`

	typeMode := &Mode{ClassName: "type", Begin: `\bi\d+\b`}
	operator := &Mode{ClassName: "operator", Begin: `=`, HasRelevance: true, Relevance: 0}
	punctuation := &Mode{ClassName: "punctuation", Begin: `,`, HasRelevance: true, Relevance: 0}

	number := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = `0[xX][a-fA-F0-9]+` }),
		Variant(number, func(m *Mode) { m.Begin = `-?\d+(?:[.]\d+)?(?:[eE][-+]?\d+(?:[.]\d+)?)?` }),
	}

	label := &Mode{ClassName: "symbol", HasRelevance: true, Relevance: 0}
	label.Variants = []*Mode{
		Variant(label, func(m *Mode) { m.Begin = `^\s*[a-z]+:` }),
	}

	variable := &Mode{ClassName: "variable"}
	variable.Variants = []*Mode{
		Variant(variable, func(m *Mode) { m.Begin = `%` + identRe }),
		Variant(variable, func(m *Mode) { m.Begin = `%\d+` }),
		Variant(variable, func(m *Mode) { m.Begin = `#\d+` }),
	}

	function := &Mode{ClassName: "title"}
	function.Variants = []*Mode{
		Variant(function, func(m *Mode) { m.Begin = `@` + identRe }),
		Variant(function, func(m *Mode) { m.Begin = `@\d+` }),
		Variant(function, func(m *Mode) { m.Begin = `!` + identRe }),
		Variant(function, func(m *Mode) { m.Begin = `!\d+` + identRe }),
		Variant(function, func(m *Mode) { m.Begin = `!\d+` }),
	}

	// The reference's `COMMENT(/;\s*$/, null, {relevance:0})` matches an empty
	// comment: end is null, so the compiler defaults it to `\B|\b`.
	emptyComment := CommentMode(`;\s*$`, `\B|\b`)
	emptyComment.HasRelevance = true
	emptyComment.Relevance = 0

	stringBase := &Mode{ClassName: "string"}
	stringBase.Variants = []*Mode{
		Variant(stringBase, func(m *Mode) { m.Begin = `"`; m.End = `[^\\]"` }),
	}

	return &Language{
		Name: "LLVM IR",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "begin end true false declare define global constant private linker_private internal " +
					"available_externally linkonce linkonce_odr weak weak_odr appending dllimport dllexport common " +
					"default hidden protected extern_weak external thread_local zeroinitializer undef null to tail target " +
					"triple datalayout volatile nuw nsw nnan ninf nsz arcp fast exact inbounds align addrspace section " +
					"alias module asm sideeffect gc dbg linker_private_weak attributes blockaddress initialexec " +
					"localdynamic localexec prefix unnamed_addr ccc fastcc coldcc x86_stdcallcc x86_fastcallcc arm_apcscc " +
					"arm_aapcscc arm_aapcs_vfpcc ptx_device ptx_kernel intel_ocl_bicc msp430_intrcc spir_func spir_kernel " +
					"x86_64_sysvcc x86_64_win64cc x86_thiscallcc cc c signext zeroext inreg sret nounwind noreturn " +
					"noalias nocapture byval nest readnone readonly inlinehint noinline alwaysinline optsize ssp sspreq " +
					"noredzone noimplicitfloat naked builtin cold nobuiltin noduplicate nonlazybind optnone returns_twice " +
					"sanitize_address sanitize_memory sanitize_thread sspstrong uwtable returned type opaque eq ne slt " +
					"sgt sle sge ult ugt ule uge oeq one olt ogt ole oge ord uno ueq une x acq_rel acquire alignstack " +
					"atomic catch cleanup filter inteldialect max min monotonic nand personality release seq_cst " +
					"singlethread umax umin unordered xchg add fadd sub fsub mul fmul udiv sdiv fdiv urem srem frem shl " +
					"lshr ashr and or xor icmp fcmp phi call trunc zext sext fptrunc fpext uitofp sitofp fptoui fptosi " +
					"inttoptr ptrtoint bitcast addrspacecast select va_arg ret br switch invoke unwind unreachable " +
					"indirectbr landingpad resume malloc alloca free load store getelementptr extractelement " +
					"insertelement shufflevector getresult extractvalue insertvalue atomicrmw cmpxchg fence argmemonly " +
					"double",
			},
			Contains: []*Mode{
				typeMode,
				emptyComment,
				CommentMode(`;`, `$`),
				QuoteStringMode,
				stringBase,
				function,
				punctuation,
				operator,
				variable,
				label,
				number,
			},
		},
	}
}
