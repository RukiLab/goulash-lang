package main

// guiBuiltinNames lists the GUI-related builtins (always registered).
// Referenced by TestBuiltinRegistryComplete.
var guiBuiltinNames = []string{
	"screen", "gsel",
	"pset", "line", "boxf", "circle",
	"gcopy", "gmode", "picload",
	"pngsave", "galpha", "paint", "font",
	"getkey", "mousex", "mousey", "clicked", "mousewheel", "fullscreen", "closing", "screensize", "cursor",
	"padcount", "padbtn", "padaxis", "padname", "resizable",
	"touchcount", "touchx", "touchy", "winmove", "dropfiles", "dropload",
	"mmload", "mmplay", "mmstop", "mmvol",
	"button", "pressed", "inputbox", "gettext", "ime", "imeget",
	"listbox", "selected", "clrobj", "dialog",
	"chkbox", "checked", "combox", "mesbox", "getstr", "setstr", "objprm",
}
