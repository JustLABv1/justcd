package publishing

func renderOrPanic(c Config) map[string]string {
	out, err := RenderConfig(c, SourceFacts{})
	if err != nil {
		panic(err)
	}
	return out
}

func pipeline(c Config) string        { return renderOrPanic(c)[FilePipeline] }
func manifest(c Config, _ int) string { return renderOrPanic(c)[FileDeployment] }
