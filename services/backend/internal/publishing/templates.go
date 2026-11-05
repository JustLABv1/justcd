package publishing

// Template describes a supported source-to-container recipe. Platform image
// references and credentials remain explicit configuration for the destination.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Runtime     string `json:"runtime"`
	Category    string `json:"category"`
	EntryPoint  string `json:"entryPoint"`
	Detail      string `json:"detail"`
	Version     string `json:"version"`
	// Port is the container port. Zero selects the runtime default.
	Port int `json:"port,omitempty"`
	// Layout lists the files an uploaded ZIP must contain. Empty keeps the
	// built-in check of the runtime.
	Layout []LayoutRule `json:"layout,omitempty"`
	// Custom marks workspace-owned templates; BaseTemplate names the template
	// a custom template was copied from.
	Custom       bool   `json:"custom,omitempty"`
	BaseTemplate string `json:"baseTemplate,omitempty"`
}

// TemplateLookup resolves a template ID, for example from the built-in list
// plus a workspace's own templates.
type TemplateLookup func(id string) (Template, bool)

// BuiltinLookup resolves only the templates shipped with JustCD.
func BuiltinLookup(id string) (Template, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

func Templates() []Template {
	return []Template{
		{ID: "shiny", Name: "Shiny", Description: "Turn your R analysis into an interactive web application. Bring your code and R dependencies.", Runtime: "shiny", Category: "R", EntryPoint: ".", Detail: "R · Shiny · port 3838", Version: Version},
		{ID: "streamlit", Name: "Streamlit", Description: "Share Python dashboards, data explorations, and machine learning tools as a web application.", Runtime: "streamlit", Category: "Python", EntryPoint: "app.py", Detail: "Python · Streamlit · port 8501", Version: Version},
		{ID: "static", Name: "Static website", Description: "Publish ready-made HTML, CSS, JavaScript, images, and PDFs without a build step.", Runtime: "static", Category: "Websites", EntryPoint: ".", Detail: "Static files · web server · port 8080", Version: Version},
		{ID: "python-static", Name: "Static website · Python", Description: "Generate HTML, CSS, and PDFs with Python, then publish the output as a static website.", Runtime: "python-static", Category: "Python", EntryPoint: "build_html.py", Detail: "Python build · static web server · port 8080", Version: Version},
		{ID: "shiny-geospatial", Name: "Shiny Geospatial", Description: "Publish maps and regional statistics with Shiny, sf, and Leaflet using your platform’s geospatial image.", Runtime: "shiny", Category: "Geospatial", EntryPoint: ".", Detail: "R · GDAL / GEOS / PROJ · port 3838", Version: Version},
	}
}
