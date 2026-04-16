package main

func startOutput(rt *Runtime) map[string]interface{} {
	return map[string]interface{}{
		"status":  "started",
		"session": rt.Endpoint,
		"gpu":     rt.Accelerator,
	}
}

func basicStatusOutput(rt *Runtime) map[string]interface{} {
	return map[string]interface{}{
		"gpu":       rt.Accelerator,
		"connected": true,
	}
}
