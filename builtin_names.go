package main

// guiBuiltinNames lists builtins registered only in -tags gui builds.
// Referenced by TestBuiltinRegistryComplete so the same test passes
// in both configurations.
var guiBuiltinNames = []string{
	"screen", "width", "gsel",
	"pset", "line", "boxf", "circle",
	"gcopy", "gmode", "picload",
	"pngsave", "gzoom", "grotate", "galpha", "paint", "font",
	"getkey", "keychar", "stick", "mousex", "mousey", "clicked", "mousewheel", "fullscreen", "closing", "screensize", "cursor",
	"padcount", "padbtn", "padaxis", "padname",
	"touchcount", "touchx", "touchy", "winmove", "dropfiles", "dropload",
	"mmload", "mmplay", "mmstop", "mmvol",
	"button", "pressed", "inputbox", "gettext", "ime", "imeget",
	"listbox", "selected", "clrobj", "dialog",
	"chkbox", "checked", "combox", "mesbox", "getstr", "setstr", "objprm",
}
