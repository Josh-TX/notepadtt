import { ApplicationConfig } from '@angular/core';
import { provideRouter } from '@angular/router';

import { routes } from './app.routes';
import { provideHttpClient } from '@angular/common/http';
import { provideAnimations } from '@angular/platform-browser/animations';
import { IRealTimeService, MockRealTimeService, SignalRRealTimeService } from './services/real-time.service';
import { ApiFileLoaderService, IFileLoaderService, MockFileLoaderService } from './services/file-loader.service';
import { environment } from '../environments/environment';

export const appConfig: ApplicationConfig = {
    providers: [
        provideRouter(routes),
        provideHttpClient(),
        provideAnimations(),
        {
            provide: IRealTimeService,
            useClass: environment.useMockService ? MockRealTimeService : SignalRRealTimeService
        },
        {
            provide: IFileLoaderService,
            useClass: environment.useMockService ? MockFileLoaderService : ApiFileLoaderService
        }
    ]
};
