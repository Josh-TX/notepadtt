import { Component, ViewChild, Inject, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterOutlet } from '@angular/router';
import { minimalSetup } from "codemirror";
import { Compartment, EditorState, Extension, StateEffect } from '@codemirror/state';
import { EditorView, lineNumbers, highlightActiveLineGutter, 
    highlightActiveLine,
    highlightSpecialChars,
    drawSelection,
    keymap,
} from '@codemirror/view';
import { markdown } from '@codemirror/lang-markdown';
import { DOCUMENT } from '@angular/common';
import {
    oneDark
} from './dark-theme';
import {
    oneLight
} from './light-theme';
import { IRealTimeService } from '../services/real-time.service';
import { FooterService } from '../services/footer.service';
import { undo, redo, history, historyField, defaultKeymap, historyKeymap, } from "@codemirror/commands";

@Component({
    selector: 'editor',
    standalone: true,
    imports: [CommonModule, RouterOutlet],
    templateUrl: './editor.component.html'
})
export class EditorComponent {

    private wordWrapCompartment = new Compartment();
    private historyCompartment = new Compartment();
    private view: EditorView | undefined;
    private init: boolean = false;
    private dispatching: boolean = false;
    private wordWrap: boolean = false;
    private lastFileId: string | undefined;

    @ViewChild('myeditor') myEditor: any;
    constructor(
        @Inject(DOCUMENT) private document: Document, 
        private realTimeService: IRealTimeService,
        private footerService: FooterService
        ) 
    { 
        effect(() => {
            //this angular effect runs each time this.signalR.$tabContent() changes
            var tabContent = this.realTimeService.$tabContent();
            if (tabContent){
                if (this.view && tabContent.fileId == this.lastFileId){
                    var oldText = this.view.state.doc.toString();
                    var newText = tabContent.text;
                    if (oldText == newText){
                        return;
                    }
                    //We only want to replace what changed, since that's less disruptive of the cursor position
                    var startMatch = getMatchLength(oldText, newText);
                    var endMatch = getMatchLength(reverseString(oldText), reverseString(newText));
                    this.dispatching = true;
                    if (startMatch + endMatch <= oldText.length){
                        this.view.dispatch({
                            changes: {
                                from: startMatch, 
                                to: oldText.length - endMatch, 
                                insert: newText.slice(startMatch, newText.length - endMatch)
                            }
                        });
                    } else {
                        //this could happen if oldText was hi\n and newtext was hi\nbye\n 
                        //^that'd match 3 at the start and 1 at the end, despite oldText having a length of just 3
                        //in this case, we keep the common start part, and replace everything after with newText
                        this.view.dispatch({
                            changes: {from: startMatch, to: oldText.length, insert: newText.slice(startMatch)}
                        });
                    }
                    this.dispatching = false;

                    //the undo history gets very confusing when there's an external edit, and so it's probably best to clear it
                    //it's unlikely that the user intends to undo an external edit, but that's what would happen if we didn't clear it
                    this.clearUndoHistory();
                } else {
                    this.lastFileId = tabContent.fileId;
                    this.renderText(tabContent.text);
                }
            } 
        }, { allowSignalWrites: true });
        effect(() => {
            this.wordWrap = this.footerService.$wordWrap();
            this.updateWordWrap();
        }, { allowSignalWrites: true });
        this.footerService.registerUndoHandler(() => {
            undo(this.view!);
        });
        this.footerService.registerRedoHandler(() => {
            redo(this.view!);
        });
    }

    private updateWordWrap(){
        if (this.view){
            this.view.dispatch({
                effects: [this.wordWrapCompartment.reconfigure(
                    this.wordWrap ? EditorView.lineWrapping : []
                )]
            });
        }
    }

    private clearUndoHistory() {
        if (this.view){
            this.view.dispatch({
                effects: this.historyCompartment.reconfigure([]) //first remove history() to clear it!!
            });
            this.view.dispatch({
                effects: this.historyCompartment.reconfigure([history()])
            });
            setTimeout(() => this.footerService.$canUndo.set(false));
        }
    }

    ngAfterViewInit(){
        this.init = true;
    }
    
    renderText(text: string){
        if (!this.init){
            setTimeout(() => this.renderText(text));
            return;
        }
        if (this.view){
            let newState = EditorState.create({
                doc: text,
                extensions: this.getExtension(),
            });
            this.view.setState(newState);
        } else {
            //first render
            var theme: any = oneLight
            if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
                theme = oneDark;
            }
            let myEditorElement = this.myEditor.nativeElement;
            let state = EditorState.create({
                doc: text,
                extensions: this.getExtension(),
            });
            this.view = new EditorView({
                state,
                parent: myEditorElement,
            });
        }
    }

    private getExtension(): Extension {
        var theme: any = oneLight
        if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
            theme = oneDark;
        }
        var wordWrapExt: Extension[] = [];
        if (this.wordWrap){
            wordWrapExt.push(EditorView.lineWrapping);
        }
        var extension: Extension[] = [
            highlightSpecialChars(),
            this.historyCompartment.of(history()),
            drawSelection(),
            keymap.of([
                ...defaultKeymap,
                ...historyKeymap,
            ]),
            lineNumbers(),
            highlightActiveLineGutter(),
            highlightActiveLine(),
            theme,
            markdown(),
            this.wordWrapCompartment.of(wordWrapExt),
            EditorView.updateListener.of(update  => {
                if (update.docChanged && !this.dispatching) {
                    var text = update.state.doc.toString();
                    var tabContent = this.realTimeService.$tabContent();
                    if (tabContent){
                        var fileId = tabContent.fileId;
                        this.realTimeService.tabContentChanged({fileId: fileId, text: text});

                        var historyState = <{done: any[], undone: any[]}>this.view!.state.field(historyField);
                        //for some reason, the done stack will have 1 extra item that's inserted upon focusing in the editor. 
                        this.footerService.$canUndo.set(historyState.done.length > 1);
                        this.footerService.$canRedo.set(historyState.undone.length > 0);
                    }
                }
                if (update.changes){
                    this.updateFooter();
                }
            })
        ];
        return extension
    }

    private updateFooter(){
        if (!this.view){
            return;
        }
        var state = this.view.state;
        var doc = state.doc;
        var selection = state.selection.main
        var toLine = doc.lineAt(selection.to);
        this.footerService.$footerData.set({
            length: doc.length,
            lines: doc.lines,
            ln: toLine.number,
            col: 1 + selection.to - toLine.from,
            pos: selection.to,
            selectedLength: selection.to > selection.from ? selection.to - selection.from : undefined,
            selectedLines: selection.to > selection.from 
                ? 1 + toLine.number - doc.lineAt(selection.from).number
                : undefined,
        })
    }
}

function reverseString(str: string) {
    return str.split("").reverse().join("");
}

function getMatchLength(str1: string, str2: string): number {
    var i = 0;
    while (true){
        if (i >= str1.length || i >= str2.length){
            return i;
        }
        if (str1[i] != str2[i]){
            return i;
        }
        i++;
    }
}


function syntaxHighlighting(defaultHighlightStyle: any, arg1: { fallback: boolean; }): Extension {
    throw new Error('Function not implemented.');
}

