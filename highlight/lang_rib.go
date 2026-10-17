package highlight

func init() { RegisterLanguage("rib", RIB()) }

// RIB returns the RenderMan RIB language definition (a port of
// languages/rib.js).
func RIB() *Language {
	return &Language{
		Name: "RenderMan RIB",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "ArchiveRecord AreaLightSource Atmosphere Attribute AttributeBegin AttributeEnd Basis " +
					"Begin Blobby Bound Clipping ClippingPlane Color ColorSamples ConcatTransform Cone " +
					"CoordinateSystem CoordSysTransform CropWindow Curves Cylinder DepthOfField Detail " +
					"DetailRange Disk Displacement Display End ErrorHandler Exposure Exterior Format " +
					"FrameAspectRatio FrameBegin FrameEnd GeneralPolygon GeometricApproximation Geometry " +
					"Hider Hyperboloid Identity Illuminate Imager Interior LightSource " +
					"MakeCubeFaceEnvironment MakeLatLongEnvironment MakeShadow MakeTexture Matte " +
					"MotionBegin MotionEnd NuPatch ObjectBegin ObjectEnd ObjectInstance Opacity Option " +
					"Orientation Paraboloid Patch PatchMesh Perspective PixelFilter PixelSamples " +
					"PixelVariance Points PointsGeneralPolygons PointsPolygons Polygon Procedural Projection " +
					"Quantize ReadArchive RelativeDetail ReverseOrientation Rotate Scale ScreenWindow " +
					"ShadingInterpolation ShadingRate Shutter Sides Skew SolidBegin SolidEnd Sphere " +
					"SubdivisionMesh Surface TextureCoordinates Torus Transform TransformBegin TransformEnd " +
					"TransformPoints Translate TrimCurve WorldBegin WorldEnd",
			},
			Illegal: `</`,
			Contains: []*Mode{
				HashCommentMode,
				CNumberMode,
				AposStringMode,
				QuoteStringMode,
			},
		},
	}
}
