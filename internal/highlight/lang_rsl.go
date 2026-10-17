package highlight

func init() { RegisterLanguage("rsl", RSL()) }

// RSL returns the RenderMan RSL language definition (a port of
// languages/rsl.js).
func RSL() *Language {
	return &Language{
		Name: "RenderMan RSL",
		Mode: Mode{
			Keywords: Keywords{
				"keyword":  "float color point normal vector matrix while for if do return else break extern continue",
				"built_in": "abs acos ambient area asin atan atmosphere attribute calculatenormal ceil cellnoise clamp comp concat cos degrees depth Deriv diffuse distance Du Dv environment exp faceforward filterstep floor format fresnel incident length lightsource log match max min mod noise normalize ntransform opposite option phong pnoise pow printf ptlined radians random reflect refract renderinfo round setcomp setxcomp setycomp setzcomp shadow sign sin smoothstep specular specularbrdf spline sqrt step tan texture textureinfo trace transform vtransform xcomp ycomp zcomp",
			},
			Illegal: `</`,
			Contains: []*Mode{
				CLineCommentMode,
				CBlockCommentMode,
				QuoteStringMode,
				AposStringMode,
				CNumberMode,
				{ClassName: "meta", Begin: `#`, End: `$`},
				{ClassName: "class", BeginKeywords: "surface displacement light volume imager", End: `\(`},
				{BeginKeywords: "illuminate illuminance gather", End: `\(`},
			},
		},
	}
}
