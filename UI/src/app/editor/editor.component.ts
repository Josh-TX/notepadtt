import { Component, ViewChild, Inject, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterOutlet } from '@angular/router';
import { minimalSetup } from "codemirror";
import { EditorState, Extension } from '@codemirror/state';
import { EditorView, lineNumbers, highlightActiveLineGutter, 
    highlightActiveLine
} from '@codemirror/view';
import { markdown } from '@codemirror/lang-markdown';
import { DOCUMENT } from '@angular/common';
import {
    oneDark
} from './dark-theme';
import {
    oneLight
} from './light-theme';
import { SignalRService } from '../services/signal-r.service';


@Component({
    selector: 'editor',
    standalone: true,
    imports: [CommonModule, RouterOutlet],
    templateUrl: './editor.component.html'
})
export class EditorComponent {

    private view: EditorView | undefined;
    private extension: Extension | undefined;
    private init: boolean = false;
    private dispatching: boolean = false;

    @ViewChild('myeditor') myEditor: any;
    constructor(
        @Inject(DOCUMENT) private document: Document, 
        private signalR: SignalRService
        ) 
    { 
        effect(() => {
            //this angular effect runs each time this.signalR.$tabContent() changes
            var tabContent = this.signalR.$tabContent();
            if (tabContent){
                if (this.view){
                    var oldText = this.view.state.doc.toString();
                    var newText = tabContent.text;
                    if (oldText == newText){
                        return;
                    }
                    //We only want to replace what changed, since that's less disruptive of the cursor position
                    var startMatch = getMatchLength(oldText, newText);
                    var endMatch = getMatchLength(reverseString(oldText), reverseString(newText));
                    this.dispatching = true;
                    if (startMatch + endMatch < newText.length){
                        this.view.dispatch({
                            changes: {
                                from: startMatch, 
                                to: oldText.length - endMatch, 
                                insert: newText.slice(startMatch, newText.length - endMatch)
                            }
                        });
                    } else {
                        //this could happen if oldText was ABA, and newText was ABABA
                        this.view.dispatch({
                            changes: {from: 0, to: oldText.length, insert: newText}
                        });
                    }
                    this.dispatching = false;
                } else {
                    this.renderText(tabContent.text);
                }
            } 
        });
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
                extensions: this.extension,
            });
            this.view.setState(newState);
            return
        } else {
            //first render
            var theme: any = oneLight
            if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
                theme = oneDark;
            }
            let myEditorElement = this.myEditor.nativeElement;
            this.extension = [
                minimalSetup,
                lineNumbers(),
                highlightActiveLineGutter(),
                highlightActiveLine(),
                theme,
                markdown(),
                EditorView.updateListener.of(z => {
                    if (z.docChanged && !this.dispatching) {
                        var text = z.state.doc.toString();
                        var tabContent = this.signalR.$tabContent();
                        if (tabContent){
                            var fileId = tabContent.fileId;
                            this.signalR.tabContentChanged({fileId: fileId, text: text});
                        }
                    }
                })
            ];
            let state = EditorState.create({
                doc: text,
                extensions: this.extension,
            });
            this.view = new EditorView({
                state,
                parent: myEditorElement,
            });
        }
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


