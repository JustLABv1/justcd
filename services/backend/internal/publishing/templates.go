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
}

func Templates() []Template {
	return []Template{
		{"shiny", "Shiny", "Turn your R analysis into an interactive web application. Bring your code and R dependencies.", "shiny", "R", ".", "R · Shiny · port 3838", Version},
		{"streamlit", "Streamlit", "Share Python dashboards, data explorations, and machine learning tools as a web application.", "streamlit", "Python", "app.py", "Python · Streamlit · port 8501", Version},
		{"static", "Static website", "Publish ready-made HTML, CSS, JavaScript, images, and PDFs without a build step.", "static", "Websites", ".", "Static files · web server · port 8080", Version},
		{"python-static", "Static website · Python", "Generate HTML, CSS, and PDFs with Python, then publish the output as a static website.", "python-static", "Python", "build_html.py", "Python build · static web server · port 8080", Version},
		{"shiny-geospatial", "Shiny Geospatial", "Publish maps and regional statistics with Shiny, sf, and Leaflet using your platform’s geospatial image.", "shiny", "Geospatial", ".", "R · GDAL / GEOS / PROJ · port 3838", Version},
	}
}
