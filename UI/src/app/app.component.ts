import { Component, ViewChild, Inject, Signal, computed, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterOutlet } from '@angular/router';
import { TabsetComponent } from './tabset/tabset.component';
import { EditorComponent } from './editor/editor.component';
import { FooterComponent } from './footer/footer.component';
import { SignalRService, Info } from './services/signal-r.service';
import { animate, style, transition, trigger } from '@angular/animations';

@Component({
    selector: 'app-root',
    standalone: true,
    imports: [CommonModule, RouterOutlet, TabsetComponent, EditorComponent, FooterComponent],
    templateUrl: './app.component.html',
    animations: [
        trigger(
            'inOutAnimation',
            [
                transition(
                    ':enter',
                    [
                        style({ bottom: 0, opacity: 0 }),
                        animate('0.25s ease-out',
                            style({ bottom: "3vh", opacity: 1 }))
                    ]
                ),
                transition(
                    ':leave',
                    [
                        style({ bottom: "3vh", opacity: 1 }),
                        animate('0.25s ease-in',
                            style({ bottom: 0, opacity: 0 }))
                    ]
                )
            ]
        )
    ]
})
export class AppComponent {
    $info: Signal<Info | null>;
    errorMessage: string | undefined;
    reconnected: boolean = false;
    private errorState = false;
    private errorMessageTimeout: any;
    constructor(private signalRService: SignalRService) {
        this.$info = this.signalRService.$info;
        effect(() => {
            var error = this.signalRService.$errorMessage();
            if (error) {
                this.errorMessage = error.message;
                this.errorState = true;
                clearTimeout(this.errorMessageTimeout);
                this.errorMessageTimeout = setTimeout(() => {
                    this.errorMessage = undefined;
                }, 3500);
            } else if (this.errorState) {
                this.errorState = false;
                this.errorMessage = undefined;
                this.reconnected = true;
                setTimeout(() => {
                    this.reconnected = false;
                }, 3500);
            }
        });
    }
}


