#!/usr/bin/python3
import subprocess
import sys

import gi

gi.require_version("AyatanaAppIndicator3", "0.1")
gi.require_version("Gtk", "3.0")
from gi.repository import AyatanaAppIndicator3, Gtk


def open_courses(_item):
    subprocess.Popen(
        ["xdg-open", sys.argv[1]],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


indicator = AyatanaAppIndicator3.Indicator.new(
    "drillbox",
    sys.argv[2],
    AyatanaAppIndicator3.IndicatorCategory.APPLICATION_STATUS,
)
indicator.set_status(AyatanaAppIndicator3.IndicatorStatus.ACTIVE)
indicator.set_title("Drillbox")

menu = Gtk.Menu()
open_item = Gtk.MenuItem(label="Open Courses")
open_item.connect("activate", open_courses)
menu.append(open_item)
menu.append(Gtk.SeparatorMenuItem())
stop_item = Gtk.MenuItem(label="Stop & Close")
stop_item.connect("activate", lambda _item: Gtk.main_quit())
menu.append(stop_item)
menu.show_all()
indicator.set_menu(menu)
Gtk.main()
