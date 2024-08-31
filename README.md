# Notepad

A simple notepad app similar to notepad++. Notes are auto-saved and synchronized in real time. Written in .NET and Angular, and heavily utilizes SignalR. 


# Installation

This app is intended to be run with docker. The simplest usage is
```
docker run -p 8080:8080 joshtxdev/notepadtt
```
The data will save to the container's filesystem, which is probably fine for most use cases. 

Optionally, you can mount a docker volume or host filesystem path the containers's `/data` directory
```
docker run -p 8080:8080 -v notepadVolume:/data joshtxdev/notepadtt
```
if mounting a filesystem path, make sure you have correct permissions for the container to read and write. 

Lastly, you can specify a `title` environemental variable to set the HTML's `<title>` (this controls the browser tab title). 
```
docker run -p 8080:8080 -e "title=temp notes" joshtxdev/notepadtt
```

# Usage

The UI is extremely minimalistic. Each file has its own tab. Right-clicking a tab will show a context menu with various actions. The `+` icon in the top right creates a new file, and the tab's `×` icon will delete the file. Because the files are deleted rather than closed, there are 2 features to mitigate accidental data loss:

1. Deleting a file will briefly display a prompt at the bottom of the screen to `UNDO` the delete.
2. You can right click a tab and choose to `protect` the file. This hides the tab's `×` icon, and the context menu's `delete` option will ask for confirmation before deleting. 

Files that were not created by the app will initially be `protect`ed.

All actions (typing, pasting, changing the active tab, renaming a file, etc.) immediately save to the server and synchronize with all other browsers.