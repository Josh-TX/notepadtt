# Notepadtt

A simple notepad app inspired by notepad++. Notes are auto-saved and synchronized in real time. Written in .NET and Angular, and heavily utilizes SignalR. A live demo is available at https://josh-tx.github.io/notepadtt/, though the demo uses localStorage rather than synchronizing with a server. 

<p align="center">
  <br>
    <img src="UI/screenshot.png" height="360"/>
  <br>
</p>

# Installation

This app is intended to be run with docker. The simplest usage is
```
docker run -p 8080:8080 joshtxdev/notepadtt
```
The data will save to the container's filesystem, which is probably fine for most use cases. 

Optionally, you can mount a docker volume (or host filesystem path) to the containers's `/data` directory
```
docker run -p 8080:8080 -v notepadVolume:/data joshtxdev/notepadtt
```
if mounting a filesystem path, make sure you have correct file permissions set up. One way to fix permissions is by adding a -u argument (such as `-u $(id -u):$(id -g)`), though this doesn't work with rootless containers. 

here's a full list of supported environmental variables

| variable name | effect |
| -------- | ------- |
| TITLE |  sets the HTML's `<title>`, giving the browser tab an alternate title. |
| MAXKB | changes the maximum file size allowed (default 200). When mounting a filesystem path to `/data`, file |
| INCLUDE_FILES | when mounting a filesystem path to `/data`, this is a glob pattern for whitelisting what files show up |
| EXCLUDE_FILES | when mounting a filesystem path to `/data`, this is a glob pattern for blacklisting what files show up. Has higher precedence than the whitelist |

You'll need to specify the environmental variable in the docker run command. For example, this sets the website's title to "temp notes"

```
docker run -p 8080:8080 -e "TITLE=temp notes" joshtxdev/notepadtt
```

# Usage

The UI is extremely minimalistic. Each file has its own tab. Right-clicking a tab will show a context menu with various actions. The `+` icon in the top right creates a new file, and the tab's `×` icon will delete the file. Because the files are deleted rather than closed, there are 2 features to mitigate accidental data loss:

1. Deleting a file will briefly display a prompt at the bottom of the screen to `UNDO` the delete. Upon clicking undo, the browser essentially re-creates the file on the server. 
2. You can right click a tab and choose to `protect` the file. This hides the tab's `×` icon, and the context menu's `delete` option will ask for confirmation before deleting. 

Files that were not created by the app (this can happen when mounting to `/data`) will initially be `protect`ed.
All actions (typing, pasting, changing the active tab, renaming a file, etc.) immediately save to the server and synchronize with all other active browser connections. The notepad is decent at handling simultaneous writes to the same file (Though if there's too much going on the cursor may jump around unexpectedly).

