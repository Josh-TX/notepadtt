import { Component, ViewChild, Inject, Signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterOutlet } from '@angular/router';
import { TabsetComponent } from './tabset/tabset.component';
import { EditorComponent } from './editor/editor.component';
import { SignalRService, Info } from './services/signal-r.service';

@Component({
    selector: 'app-root',
    standalone: true,
    imports: [CommonModule, RouterOutlet, TabsetComponent, EditorComponent],
    templateUrl: './app.component.html'
})
export class AppComponent {
    $info: Signal<Info | null>;
    constructor(private signalR: SignalRService){
        this.$info = this.signalR.$info;
    }
}


